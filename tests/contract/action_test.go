package contract

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const actionFixture = `
namespace dokuwiki\Extension {
class ActionPlugin {
    public function getConf($key) {
        if (($GLOBALS['case']['badconfig'] ?? false) && $key === 'petar_url') return '';
        return ['petar_url'=>'http://backend','api_secret'=>$GLOBALS['case']['secret'] ?? '','timeout'=>15][$key];
    }
}
class Event {
    public $data = 'plugin_petar', $result;
    public function stopPropagation() {}
}
}
namespace {
class TestInput {
    public $post, $server, $get;
    public function __construct(public $values) {}
    public function param($key,$default=null) { return $this->values[$key] ?? $default; }
    public function str($key,$default='') { $v = $this->param($key,$default); return is_scalar($v) ? (string)$v : $default; }
    public function has($key) { return isset($this->values[$key]); }
}
function checkSecurityToken() { global $case; return $case['csrf'] ?? true; }
require '/config/dokuwiki/lib/plugins/petar/action.php';
`

func actionPHP(t *testing.T, tc map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, _ := json.Marshal(tc)
	// Controller exits, so shutdown captures its response and status.
	return runPHP(t, strings.TrimSuffix(clientFixture, "\n")+"\n}\n"+actionFixture[1:]+`
$case = json_decode(base64_decode('`+base64Fixture(raw)+`'),true);
$INPUT = new TestInput(['action'=>$case['action'] ?? 'submit']);
$INPUT->get = new TestInput($case['get'] ?? []);
$INPUT->post = new TestInput($case['post'] ?? ['action_name'=>'echo-test','version'=>'1','data'=>'{"message":"hello"}']);
$INPUT->server = new TestInput([
 'REMOTE_USER'=>$case['user'] ?? 'wiki', 'REQUEST_METHOD'=>$case['method'] ?? 'POST',
 'HTTP_X_REQUEST_ID'=>$case['request_id'] ?? '12345678-1234-4234-8234-123456789abc'
]);
$USERINFO = ['grps'=>$case['groups'] ?? ['ai']];
\dokuwiki\HTTP\DokuHTTPClient::$code = $case['backend_code'] ?? 201;
\dokuwiki\HTTP\DokuHTTPClient::$body = $case['backend_body'] ?? \dokuwiki\HTTP\DokuHTTPClient::$body;
\dokuwiki\HTTP\DokuHTTPClient::$ok = $case['backend_ok'] ?? true;
ob_start();
register_shutdown_function(function() {
 $body = ob_get_clean();
 echo json_encode(['code'=>http_response_code(),'body'=>json_decode($body),
     'sent'=>\dokuwiki\HTTP\DokuHTTPClient::$sent,'logs'=>\dokuwiki\Logger::$messages]);
});
$event = new \dokuwiki\Extension\Event();
$event->data = $case['call'] ?? 'plugin_petar';
(new \action_plugin_petar())->handleAjax($event);
}`)
}

func base64Fixture(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func TestActionsCatalog(t *testing.T) {
	const backend = `[{"action":"fabric","version":1,"title":"Fabric"},{"action":"fabric","version":2,"title":"Fabric v2"}]`
	got := actionPHP(t, map[string]interface{}{
		"action": "actions", "method": "GET", "request_id": directRequestID,
		"backend_code": 200, "backend_body": backend,
	})
	if got["code"] != float64(200) {
		t.Fatalf("catalog status: got %v, want 200 (%#v)", got["code"], got)
	}
	var rows interface{}
	if err := json.Unmarshal([]byte(backend), &rows); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"actions": rows, "request_id": directRequestID}
	if !reflect.DeepEqual(got["body"], want) {
		t.Fatalf("catalog: got %#v, want %#v", got["body"], want)
	}
	sent, ok := got["sent"].(map[string]interface{})
	if !ok || sent["url"] != "http://backend/api/v1/actions" || sent["method"] != "GET" || sent["raw_body"] != "" ||
		sent["max_redirect"] != float64(0) {
		t.Fatalf("catalog transport: %#v", got["sent"])
	}
	for _, tc := range []struct {
		label string
		input map[string]interface{}
		code  float64
		sent  bool
	}{
		{"anonymous", map[string]interface{}{"user": ""}, 403, false},
		{"ordinary", map[string]interface{}{"groups": []string{"user"}}, 403, false},
		{"admin_empty", map[string]interface{}{"groups": []string{"admin"}, "backend_body": "[]"}, 200, true},
		{"POST", map[string]interface{}{"method": "POST"}, 405, false},
		{"csrf", map[string]interface{}{"method": "POST", "csrf": false}, 403, false},
		{"object", map[string]interface{}{"backend_body": "{}"}, 502, true},
		{"partial", map[string]interface{}{"backend_body": `[{"action":"fabric","version":1,"title":"Fabric"},{"action":"other","version":"2","title":"PRIVATE"}]`}, 502, true},
		{"redirect", map[string]interface{}{"backend_code": 302, "backend_body": backend}, 502, true},
		{"transport_failure", map[string]interface{}{"backend_ok": false}, 502, true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			input := map[string]interface{}{"action": "actions", "method": "GET", "backend_code": 200, "backend_body": backend}
			for key, value := range tc.input {
				input[key] = value
			}
			got := actionPHP(t, input)
			if got["code"] != tc.code || (got["sent"] != nil) != tc.sent {
				t.Fatalf("unexpected catalog outcome: %#v", got)
			}
			body := got["body"].(map[string]interface{})
			if tc.code == 502 && (body["error"] != "Backend request failed" || body["actions"] != nil) {
				t.Fatalf("unsafe or partial catalog: %#v", body)
			}
			if tc.label == "admin_empty" && !reflect.DeepEqual(body["actions"], []interface{}{}) {
				t.Fatalf("expected empty array, got %#v", body)
			}
		})
	}
}

func TestUniversalSubmit(t *testing.T) {
	const data = `{"n":9007199254740993,"value":[{},[],null,"a\"b"]}`
	got := actionPHP(t, map[string]interface{}{
		"post": map[string]interface{}{"action_name": "fixture-action", "version": "2", "data": data},
	})
	sent, ok := got["sent"].(map[string]interface{})
	if got["code"] != float64(201) || !ok || sent["url"] != "http://backend/api/v1/actions" ||
		sent["raw_body"] != `{"action":"fixture-action","version":2,"data":`+data+`}` {
		t.Fatalf("universal submit: %#v", got)
	}
	for _, post := range []map[string]interface{}{
		{"tool": "echo-test", "message": "old"},
		{"action_name": "", "version": "1", "data": "null"},
		{"action_name": "new", "version": "0", "data": "null"},
		{"action_name": "new", "version": "2147483648", "data": "null"},
		{"action_name": "new", "version": "1.0", "data": "null"},
		{"action_name": "new", "version": "999999999999999999999", "data": "null"},
		{"action_name": "new", "version": "1", "data": "{"},
		{"action_name": "new", "version": "1", "data": []string{"{}"}},
		{"action_name": "new", "version": "1"},
	} {
		got := actionPHP(t, map[string]interface{}{"post": post})
		if got["code"] != float64(400) || got["sent"] != nil {
			t.Errorf("invalid input %#v made it past BFF: %#v", post, got)
		}
	}
}

func TestJobResultAction(t *testing.T) {
	t.Run("selected_job", func(t *testing.T) {
		id := jobsID(2)
		got := actionPHP(t, map[string]interface{}{
			"action": "job_result", "method": "GET", "request_id": directRequestID,
			"get":          map[string]interface{}{"job_id": id, "process_id": jobsID(500)},
			"post":         map[string]interface{}{"job_id": jobsID(1)},
			"backend_code": 200, "backend_body": `{"type":"echo","version":1,"data":{"marker":"second-job"}}`,
		})
		want := map[string]interface{}{
			"job_id": id, "request_id": directRequestID,
			"result": map[string]interface{}{"type": "echo", "version": float64(1), "data": map[string]interface{}{"marker": "second-job"}},
		}
		if got["code"] != float64(200) || !reflect.DeepEqual(got["body"], want) {
			t.Fatalf("expected HTTP 200 for selected job %s, got %#v", id, got)
		}
		sent := got["sent"].(map[string]interface{})
		if sent["method"] != "GET" || sent["raw_body"] != "" ||
			sent["url"] != "http://backend/api/v1/jobs/"+id+"/result" ||
			sent["max_redirect"] != float64(0) || sent["debug"] != false {
			t.Fatalf("wrong upstream request: %#v", sent)
		}
		headers := sent["headers"].(map[string]interface{})
		if headers["X-Request-ID"] != directRequestID || headers["X-Wiki-User"] != "wiki" || headers["X-API-Key"] != nil {
			t.Fatalf("wrong upstream headers: %#v", headers)
		}
	})
	request := func(t *testing.T, args map[string]interface{}) map[string]interface{} {
		t.Helper()
		args["action"] = "job_result"
		if args["method"] == nil {
			args["method"] = "GET"
		}
		return actionPHP(t, args)
	}
	t.Run("admin", func(t *testing.T) {
		got := request(t, map[string]interface{}{"user": "admin-user", "groups": []string{"admin"}, "secret": "fixture-key",
			"get": map[string]interface{}{"job_id": jobsID(2), "user": "forged"}, "backend_code": 200,
			"backend_body": `{"type":"echo","version":1,"data":{}}`})
		if got["code"] != float64(200) {
			t.Fatalf("admin denied: %#v", got)
		}
		headers := got["sent"].(map[string]interface{})["headers"].(map[string]interface{})
		if headers["X-Wiki-User"] != "admin-user" || headers["X-API-Key"] != "fixture-key" {
			t.Fatalf("wrong upstream headers: %#v", headers)
		}
	})
	for _, tc := range []struct {
		name string
		args map[string]interface{}
		code int
	}{
		{"anonymous", map[string]interface{}{"user": ""}, 403},
		{"ordinary", map[string]interface{}{"groups": []string{"user"}}, 403},
		{"bad_csrf", map[string]interface{}{"method": "POST", "csrf": false}, 403},
		{"post", map[string]interface{}{"method": "POST"}, 405},
		{"missing", map[string]interface{}{"post": map[string]interface{}{"job_id": jobsID(2)}}, 400},
		{"array", map[string]interface{}{"get": map[string]interface{}{"job_id": []string{jobsID(2)}}}, 400},
		{"invalid_id", map[string]interface{}{"get": map[string]interface{}{"job_id": "not-uuid"}}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request(t, tc.args)
			if got["code"] != float64(tc.code) || got["sent"] != nil ||
				got["body"].(map[string]interface{})["request_id"] != directRequestID {
				t.Fatalf("expected HTTP %d without upstream: %#v", tc.code, got)
			}
		})
	}
	const empty = `{"type":"echo","version":1,"data":{}}`
	const capBytes = 1 << 20
	for _, tc := range []struct {
		name, raw, key, value string
		upstream, code        int
		ok                    bool
	}{
		{"empty_object", empty, "", "", 200, 200, true},
		{"at_limit", empty + strings.Repeat(" ", capBytes-len(empty)), "", "", 200, 200, true},
		{"over_limit", empty + strings.Repeat(" ", capBytes-len(empty)+1), "error", "Backend request failed", 200, 502, true},
		{"pending", `{"status":"pending","diagnostic":"SECRET"}`, "status", "pending", 202, 202, true},
		{"dispatched", `{"status":"dispatched"}`, "status", "dispatched", 202, 202, true},
		{"in_progress", `{"status":"in_progress"}`, "status", "in_progress", 202, 202, true},
		{"failed", `{"error":"job_failed","diagnostic":"SECRET"}`, "error", "job_failed", 409, 409, true},
		{"missing_job", `{"error":"SECRET"}`, "error", "job_not_found", 404, 404, true},
		{"unavailable", `{"error":"result_unavailable"}`, "error", "result_unavailable", 410, 410, true},
		{"bad_status", `{"status":"failed"}`, "error", "Backend request failed", 202, 502, true},
		{"wrong_conflict", `{"error":"SECRET"}`, "error", "Backend request failed", 409, 502, true},
		{"wrong_gone", `{"error":"SECRET"}`, "error", "Backend request failed", 410, 502, true},
		{"extra_field", `{"type":"echo","version":1,"data":{},"diagnostic":"SECRET"}`, "error", "Backend request failed", 200, 502, true},
		{"missing_type", `{"version":1,"data":{}}`, "error", "Backend request failed", 200, 502, true},
		{"empty_type", `{"type":"","version":1,"data":{}}`, "error", "Backend request failed", 200, 502, true},
		{"bad_version", `{"type":"echo","version":2,"data":{}}`, "error", "Backend request failed", 200, 502, true},
		{"array_data", `{"type":"echo","version":1,"data":[]}`, "error", "Backend request failed", 200, 502, true},
		{"partial", `{"type":"echo","version":1,"data":{`, "error", "Backend request failed", 200, 502, true},
		{"redirect", empty, "error", "Backend request failed", 302, 502, false},
		{"too_large_upstream", `{"error":"result_too_large"}`, "error", "Backend request failed", 500, 502, false},
		{"timeout", `{"error":"SECRET"}`, "error", "Backend request failed", 504, 502, false},
		{"transport", empty, "error", "Backend request failed", 0, 502, false},
		{"interrupted_200", empty, "error", "Backend request failed", 200, 502, false},
		{"interrupted_404", "", "error", "Backend request failed", 404, 502, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request(t, map[string]interface{}{
				"get": map[string]interface{}{"job_id": jobsID(2)}, "backend_code": tc.upstream,
				"backend_body": tc.raw, "backend_ok": tc.ok,
			})
			want := map[string]interface{}{"request_id": directRequestID}
			if tc.code == 200 {
				want["job_id"] = jobsID(2)
				want["result"] = map[string]interface{}{"type": "echo", "version": float64(1), "data": map[string]interface{}{}}
			} else {
				want[tc.key] = tc.value
				if tc.code == 502 {
					want["success"] = false
				}
			}
			if got["code"] != float64(tc.code) || !reflect.DeepEqual(got["body"], want) ||
				strings.Contains(fmt.Sprint(got["logs"]), "SECRET") || got["sent"] == nil {
				t.Fatalf("expected HTTP %d with %#v, got %#v", tc.code, want, got)
			}
		})
	}
}

func TestJobsAction(t *testing.T) {
	trace := regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	request := func(args map[string]interface{}) map[string]interface{} {
		if args == nil {
			args = map[string]interface{}{}
		}
		args["action"] = "jobs"
		if args["method"] == nil {
			args["method"] = "GET"
		}
		if args["backend_body"] == nil {
			args["backend_body"] = "[]"
		}
		args["backend_code"] = 200
		return actionPHP(t, args)
	}
	var all []interface{}
	for _, n := range []int{0, 1, 20, 21} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			fixture := jobsFixture(n)
			if n >= 2 {
				fixture[1]["process_id"] = jobsID(501)
			}
			raw, _ := json.Marshal(fixture)
			users := []struct{ user, role string }{{"alice", "ai"}, {"bob", "admin"}}
			for _, u := range users {
				got := request(map[string]interface{}{"backend_body": string(raw), "user": u.user, "groups": []string{u.role},
					"get": map[string]interface{}{"user": "forged", "process_id": "forged", "limit": "1"}, "csrf": false})
				if got["code"] != float64(200) {
					t.Fatalf("jobs expected HTTP 200, got %#v", got)
				}
				body := got["body"].(map[string]interface{})
				sent := got["sent"].(map[string]interface{})
				headers := sent["headers"].(map[string]interface{})
				if len(body) != 3 || body["request_id"] != "12345678-1234-4234-8234-123456789abc" ||
					headers["X-Request-ID"] != body["request_id"] || headers["X-Wiki-User"] != u.user ||
					sent["url"] != "http://backend/api/v1/jobs?limit=21" {
					t.Fatalf("response/upstream mismatch: %#v", got)
				}
				rows := body["jobs"].([]interface{})
				count := n
				var cursor interface{}
				if n == 21 {
					count, cursor = 20, fixture[19]["id"]
				}
				if len(rows) != count || body["next_cursor"] != cursor {
					t.Fatalf("invalid page length/cursor: %d, %v", len(rows), body["next_cursor"])
				}
				for i, row := range rows {
					f := fixture[i]
					want := map[string]interface{}{"job_id": f["id"], "process_id": f["process_id"], "tool": f["tool_name"],
						"status": f["status"], "created_at": f["created_at"], "updated_at": f["updated_at"]}
					if !reflect.DeepEqual(row, want) {
						t.Fatalf("unsafe or reordered row %d: %#v", i, row)
					}
				}
				if n == 21 {
					all = rows
					rawNext, _ := json.Marshal(fixture[20:])
					next := request(map[string]interface{}{"backend_body": string(rawNext),
						"get": map[string]interface{}{"status": "completed", "cursor": cursor}})
					nextBody := next["body"].(map[string]interface{})
					nextSent := next["sent"].(map[string]interface{})
					if next["code"] != float64(200) || nextBody["next_cursor"] != nil ||
						nextSent["url"] != "http://backend/api/v1/jobs?limit=21&status=completed&cursor="+cursor.(string) {
						t.Fatalf("invalid next page: %#v", next)
					}
					all = append(all, nextBody["jobs"].([]interface{})...)
					if len(all) != 21 {
						t.Fatalf("expected 21 rows across both pages, got %d", len(all))
					}
					for i, row := range all {
						if row.(map[string]interface{})["job_id"] != fixture[i]["id"] {
							t.Fatalf("missing/duplicate row at %d", i)
						}
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		name string
		args map[string]interface{}
		code int
	}{
		{"anonymous", map[string]interface{}{"user": "", "request_id": "invalid"}, 403},
		{"ordinary", map[string]interface{}{"groups": []string{"user"}}, 403},
		{"badCSRF", map[string]interface{}{"method": "POST", "csrf": false}, 403},
		{"validCSRF", map[string]interface{}{"method": "POST"}, 405},
		{"emptyStatus", map[string]interface{}{"get": map[string]interface{}{"status": ""}}, 400},
		{"invalidStatus", map[string]interface{}{"get": map[string]interface{}{"status": "new"}}, 400},
		{"arrayStatus", map[string]interface{}{"get": map[string]interface{}{"status": []string{"pending"}}}, 400},
		{"emptyCursor", map[string]interface{}{"get": map[string]interface{}{"cursor": ""}}, 400},
		{"invalidCursor", map[string]interface{}{"get": map[string]interface{}{"cursor": "not-uuid"}}, 400},
		{"arrayCursor", map[string]interface{}{"get": map[string]interface{}{"cursor": []string{jobsID(1)}}}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request(tc.args)
			if got["code"] != float64(tc.code) || got["sent"] != nil ||
				!trace.MatchString(got["body"].(map[string]interface{})["request_id"].(string)) {
				t.Fatalf("expected HTTP %d before upstream, got %#v", tc.code, got)
			}
		})
	}
}

func TestBFFBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]interface{}
		code int
	}{
		{"submit", nil, 201},
		{"anonymous", map[string]interface{}{"user": ""}, 403},
		{"ordinary", map[string]interface{}{"groups": []string{"user"}}, 403},
		{"admin", map[string]interface{}{"groups": []string{"admin"}}, 201},
		{"csrf", map[string]interface{}{"csrf": false}, 403},
		{"status", map[string]interface{}{"action": "status", "method": "GET"}, 404},
		{"unknown", map[string]interface{}{"action": "unknown"}, 404},
		{"unknownCSRF", map[string]interface{}{"action": "unknown", "csrf": false}, 403},
		{"method", map[string]interface{}{"method": "GET"}, 405},
		{"readyMethod", map[string]interface{}{"action": "readyz"}, 405},
		{"nameArray", map[string]interface{}{"post": map[string]interface{}{"action_name": []string{"echo-test"}, "version": "1", "data": "{}"}}, 400},
		{"versionArray", map[string]interface{}{"post": map[string]interface{}{"action_name": "fabric", "version": []string{"1"}, "data": "{}"}}, 400},
		{"versionNumber", map[string]interface{}{"post": map[string]interface{}{"action_name": "fabric", "version": 1, "data": "{}"}}, 400},
		{"dataArray", map[string]interface{}{"post": map[string]interface{}{"action_name": "fabric", "version": "1", "data": []string{"{}"}}}, 400},
		{"actionArray", map[string]interface{}{"action": []string{"submit"}}, 400},
		{"backend", map[string]interface{}{"backend_code": 500, "backend_body": `{"error":"SECRET"}`}, 502},
		{"configuration", map[string]interface{}{"badconfig": true}, 500},
		{"ready", map[string]interface{}{"action": "readyz", "method": "GET", "backend_code": 200, "backend_body": `{"status":"ready"}`}, 200},
		{"notReady", map[string]interface{}{"action": "readyz", "method": "GET", "backend_code": 200, "backend_body": `{"status":"wrong"}`}, 503},
		{"unavailable", map[string]interface{}{"action": "readyz", "method": "GET", "backend_code": 503, "backend_body": `{"error":"SECRET"}`}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := actionPHP(t, tc.args)
			if got["code"] != float64(tc.code) {
				t.Fatalf("got %#v, want HTTP %d", got, tc.code)
			}
			body, ok := got["body"].(map[string]interface{})
			if !ok || body["request_id"] != "12345678-1234-4234-8234-123456789abc" {
				t.Fatalf("missing/changed request ID: %#v", got)
			}
			if sent, ok := got["sent"].(map[string]interface{}); ok {
				if sent["headers"].(map[string]interface{})["X-Request-ID"] != body["request_id"] {
					t.Fatal("backend request ID mismatch")
				}
			}
			raw, _ := json.Marshal(body)
			if strings.Contains(string(raw), "SECRET") {
				t.Fatal("backend secret leaked")
			}
		})
	}
}

func TestSubmitOutcomes(t *testing.T) {
	const private = "PRIVATE_UPSTREAM_BODY"
	for _, tc := range []struct {
		name, message    string
		upstream, status int
	}{
		{"rejected", "Input rejected", 400, 400},
		{"lost_response", "Backend request failed", 0, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := actionPHP(t, map[string]interface{}{
				"request_id": directRequestID, "backend_code": tc.upstream, "backend_ok": false,
				"backend_body": `{"error":"` + private + `"}`,
			})
			want := map[string]interface{}{"request_id": directRequestID, "error": tc.message, "success": false}
			if got["code"] != float64(tc.status) || !reflect.DeepEqual(got["body"], want) {
				t.Fatalf("want HTTP %d with %#v, got %#v", tc.status, want, got)
			}
			if strings.Contains(fmt.Sprint(got["logs"]), private) {
				t.Fatalf("private upstream body leaked in logs: %#v", got["logs"])
			}
		})
	}
}

func TestBFFGeneratesRequestID(t *testing.T) {
	for _, id := range []interface{}{"", "invalid", []string{"invalid"}} {
		got := actionPHP(t, map[string]interface{}{
			"request_id": id, "post": map[string]interface{}{
				"action_name": "echo-test", "version": "1", "data": `{"message":"hello"}`, "sectok": "11111111-1111-4111-8111-111111111111",
			},
		})
		body := got["body"].(map[string]interface{})
		trace, _ := body["request_id"].(string)
		if got["code"] != float64(201) || trace == "11111111-1111-4111-8111-111111111111" ||
			!regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(trace) {
			t.Fatalf("invalid/generated trace: %#v", got)
		}
	}
}

func TestForeignCallUntouched(t *testing.T) {
	got := actionPHP(t, map[string]interface{}{"call": "another"})
	if got["body"] != nil || got["sent"] != nil {
		t.Fatalf("foreign call handled: %#v", got)
	}
}

// Use real DokuWiki Input and actual HTTP headers, not CLI headers_list().
func TestBFFHTTPHeaders(t *testing.T) {
	source := "<?php\n" + clientFixture + "\n}\n" + actionFixture[1:] + `
require '/app/www/public/inc/Input/Input.php';
require '/app/www/public/inc/Input/Post.php';
require '/app/www/public/inc/Input/Get.php';
require '/app/www/public/inc/Input/Server.php';
$case = ['csrf'=>!isset($_GET['badcsrf'])];
$_SERVER['REMOTE_USER'] = isset($_GET['anon']) ? '' : 'wiki';
$INPUT = new \dokuwiki\Input\Input();
$USERINFO = ['grps'=>['ai']];
if (in_array($_GET['action'] ?? '', ['jobs', 'actions'], true)) {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 200;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '[]';
}
if (($_GET['action'] ?? '') === 'job_result') {
 \dokuwiki\HTTP\DokuHTTPClient::$code = isset($_GET['missing_job']) ? 404 : 200;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '{"type":"echo","version":1,"data":{}}';
}
if (($_GET['action'] ?? '') === 'readyz') {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 200;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '{"status":"ready"}';
}
if (isset($_GET['backend_fail'])) {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 500;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '{"error":"SECRET"}';
}
if (isset($_GET['backend_reject'])) {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 400;
 \dokuwiki\HTTP\DokuHTTPClient::$ok = false;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '{"error":"SECRET"}';
}
(new \action_plugin_petar())->handleAjax(new \dokuwiki\Extension\Event());
}`
	encoded := base64.StdEncoding.EncodeToString([]byte(source))
	script := `set -eu
dir=$(mktemp -d)
pid=''
trap 'if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi; rm -rf "$dir"' EXIT
printf '%s' '` + encoded + `' | base64 -d > "$dir/router.php"
php -S 127.0.0.1:19847 "$dir/router.php" >"$dir/server.log" 2>&1 &
pid=$!
for i in 1 2 3 4 5; do
 if curl -sf -o /dev/null http://127.0.0.1:19847/?action=readyz; then break; fi
 sleep 0.1
done
curl -sS -i "$@" http://127.0.0.1:19847/"$query"
`
	for _, tc := range []struct {
		query, method, data, id string
		code                    int
	}{
		{"?action=readyz", "GET", "", "12345678-1234-4234-8234-123456789abc", 200},
		{"?action=readyz&anon=1", "GET", "", "", 403},
		{"?action=submit&badcsrf=1", "POST", "", "", 403},
		{"?action=submit", "POST", "action_name=echo-test&version=1&data=%7B%7D", "", 201},
		{"?action=submit", "POST", "action_name[]=fabric&version=1&data=%7B%7D", "", 400},
		{"?action=submit", "POST", "action_name=%FF&version=1&data=%7B%7D", "", 400},
		{"?action=status", "GET", "", "", 404},
		{"?action=submit", "GET", "", "", 405},
		{"?action=readyz", "GET", "", "invalid", 200},
		{"?action=actions", "GET", "", directRequestID, 200},
		{"?action=actions", "POST", "", "", 405},
		{"?action=readyz&backend_fail=1", "GET", "", "", 503},
		{"?action=submit&backend_fail=1", "POST", "action_name=echo-test&version=1&data=%7B%7D", "", 502},
		{"?action=submit&backend_reject=1", "POST", "action_name=echo-test&version=1&data=%7B%7D", directRequestID, 400},
		{"?action=jobs", "GET", "", "invalid", 200},
		{"?action=jobs&status[]=failed", "GET", "", "", 400},
		{"?action=jobs&cursor=", "GET", "", "", 400},
		{"?action=jobs&backend_fail=1", "GET", "", "", 502},
		{"?action=jobs&anon=1", "GET", "", "invalid", 403},
		{"?action=jobs", "POST", "", "", 405},
		{"?action=job_result&job_id=" + jobsID(2), "GET", "", directRequestID, 200},
		{"?action=job_result&job_id=" + jobsID(2) + "&missing_job=1", "GET", "", "", 404},
		{"?action=job_result&job_id[]=" + jobsID(2), "GET", "", "", 400},
		{"?action=job_result&job_id=" + jobsID(2) + "&badcsrf=1", "POST", "", "", 403},
		{"?action=job_result&job_id=" + jobsID(2), "POST", "", "", 405},
	} {
		t.Run(fmt.Sprintf("%s/%s/%s", tc.method, tc.query, tc.data), func(t *testing.T) {
			// Arguments are separate argv; query is supplied via the shell environment.
			cmd := exec.Command("docker", "exec", "-e", "query="+tc.query, "--", wikiContainer(), "sh", "-c", script,
				"fixture", "-X", tc.method, "-H", "X-Request-ID: "+tc.id)
			if tc.method == "POST" {
				cmd.Args = append(cmd.Args, "--data", tc.data)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("HTTP fixture: %v\n%s", err, out)
			}
			resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(string(out))), nil)
			if err != nil {
				t.Fatalf("HTTP: %v\n%s", err, out)
			}
			defer resp.Body.Close()
			var body map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				dump, _ := httputil.DumpResponse(resp, true)
				t.Fatalf("JSON: %v\n%s", err, dump)
			}
			id := resp.Header.Get("X-Request-ID")
			if resp.StatusCode != tc.code || body["request_id"] != id ||
				!regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
				t.Fatalf("HTTP %d header %q body %#v", resp.StatusCode, id, body)
			}
			if tc.id != "" && tc.id != "invalid" && id != tc.id {
				t.Fatal("valid ID changed")
			}
			if tc.code == 405 {
				want := "POST"
				if strings.HasPrefix(tc.query, "?action=jobs") || strings.HasPrefix(tc.query, "?action=job_result") ||
					tc.query == "?action=actions" {
					want = "GET"
				}
				if resp.Header.Get("Allow") != want {
					t.Fatalf("expected Allow: %s", want)
				}
			}
			if (tc.query == "?action=jobs" || tc.query == "?action=actions") &&
				tc.code == 200 && resp.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("missing Cache-Control: no-store")
			}
			if strings.HasPrefix(tc.query, "?action=job_result") && tc.method == "GET" &&
				resp.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("missing Cache-Control: no-store for result")
			}
			if strings.Contains(fmt.Sprint(body), "SECRET") {
				t.Fatal("raw error leaked")
			}
		})
	}
}
