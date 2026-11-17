package contract

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os/exec"
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
    public $post, $server;
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
$case = json_decode('`+string(raw)+`',true);
$INPUT = new TestInput(['action'=>$case['action'] ?? 'submit']);
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
	} {
		t.Run(fmt.Sprintf("%s/%s/%s", tc.method, tc.query, tc.data), func(t *testing.T) {
			// Arguments are separate argv; query is supplied via the shell environment.
			cmd := exec.Command("docker", "exec", "-e", "query="+tc.query, "dokuwiki", "sh", "-c", script,
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
			if tc.code == 405 && resp.Header.Get("Allow") != "POST" {
				t.Fatal("missing Allow")
			}
		})
	}
}
