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
        return ['petar_url'=>'http://backend','api_secret'=>'','timeout'=>15][$key];
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
$INPUT->post = new TestInput($case['post'] ?? ['tool'=>'echo-test','message'=>'hello']);
$INPUT->server = new TestInput([
 'REMOTE_USER'=>$case['user'] ?? 'wiki', 'REQUEST_METHOD'=>$case['method'] ?? 'POST',
 'HTTP_X_REQUEST_ID'=>$case['request_id'] ?? '12345678-1234-4234-8234-123456789abc'
]);
$USERINFO = ['grps'=>$case['groups'] ?? ['ai']];
\dokuwiki\HTTP\DokuHTTPClient::$code = $case['backend_code'] ?? 201;
\dokuwiki\HTTP\DokuHTTPClient::$body = $case['backend_body'] ?? \dokuwiki\HTTP\DokuHTTPClient::$body;
ob_start();
register_shutdown_function(function() {
 $body = ob_get_clean();
 echo json_encode(['code'=>http_response_code(),'body'=>json_decode($body,true),'sent'=>\dokuwiki\HTTP\DokuHTTPClient::$sent]);
});
$event = new \dokuwiki\Extension\Event();
$event->data = $case['call'] ?? 'plugin_petar';
(new \action_plugin_petar())->handleAjax($event);
}`)
}

func base64Fixture(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

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
		{"array", map[string]interface{}{"post": map[string]interface{}{"tool": []string{"echo-test"}, "message": "text"}}, 400},
		{"patternArray", map[string]interface{}{"post": map[string]interface{}{"tool": "fabric", "message": "text", "pattern": []string{"summarize"}}}, 400},
		{"patternNumber", map[string]interface{}{"post": map[string]interface{}{"tool": "fabric", "message": "text", "pattern": 123}}, 400},
		{"messageArray", map[string]interface{}{"post": map[string]interface{}{"tool": "fabric", "message": []string{"text"}}}, 400},
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

func TestBFFGeneratesRequestID(t *testing.T) {
	for _, id := range []interface{}{"", "invalid", []string{"invalid"}} {
		got := actionPHP(t, map[string]interface{}{
			"request_id": id, "post": map[string]interface{}{
				"tool": "echo-test", "message": "hello", "sectok": "11111111-1111-4111-8111-111111111111",
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
if (($_GET['action'] ?? '') === 'jobs') {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 200;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '[]';
}
if (($_GET['action'] ?? '') === 'readyz') {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 200;
 \dokuwiki\HTTP\DokuHTTPClient::$body = '{"status":"ready"}';
}
if (isset($_GET['backend_fail'])) {
 \dokuwiki\HTTP\DokuHTTPClient::$code = 500;
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
		{"?action=submit", "POST", "tool=echo-test&message=hello", "", 201},
		{"?action=submit", "POST", "tool[]=fabric&message=text", "", 400},
		{"?action=submit", "POST", "tool=%FF&message=text", "", 400},
		{"?action=status", "GET", "", "", 404},
		{"?action=submit", "GET", "", "", 405},
		{"?action=readyz", "GET", "", "invalid", 200},
		{"?action=readyz&backend_fail=1", "GET", "", "", 503},
		{"?action=submit&backend_fail=1", "POST", "tool=echo-test&message=hello", "", 502},
		{"?action=jobs", "GET", "", "invalid", 200},
		{"?action=jobs&status[]=failed", "GET", "", "", 400},
		{"?action=jobs&cursor=", "GET", "", "", 400},
		{"?action=jobs&backend_fail=1", "GET", "", "", 502},
		{"?action=jobs&anon=1", "GET", "", "invalid", 403},
		{"?action=jobs", "POST", "", "", 405},
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
				if strings.HasPrefix(tc.query, "?action=jobs") {
					want = "GET"
				}
				if resp.Header.Get("Allow") != want {
					t.Fatalf("expected Allow: %s", want)
				}
			}
			if tc.query == "?action=jobs" && tc.code == 200 && resp.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("missing Cache-Control: no-store")
			}
			if strings.Contains(fmt.Sprint(body), "SECRET") {
				t.Fatal("raw error leaked")
			}
		})
	}
}
