package contract

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

const pluginRoot = "/config/dokuwiki/lib/plugins/petar/"

// Execute production PHP, replacing only the DokuWiki HTTP transport.
func runPHP(t *testing.T, source string) map[string]interface{} {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", "--", wikiContainer(), "php")
	cmd.Stdin = strings.NewReader("<?php\n" + source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PHP: %v\n%s", err, out)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("PHP JSON: %v\n%s", err, out)
	}
	return got
}

const clientFixture = `
namespace dokuwiki {
class Logger { public static $messages = []; public static function error(...$args) { self::$messages[] = $args[1]; } }
}
namespace dokuwiki\HTTP {
class DokuHTTPClient {
    public $timeout, $status = 201, $resp_body, $error = '', $headers = [], $max_redirect = 5, $debug = true;
    public $keep_alive = true, $max_bodysize = 0;
    public static $code = 201, $body = '{"input_id":"019b2c5d-1234-7000-8000-123456789abc","process_id":"019b2c5d-1234-7000-8000-123456789abd","status":"new"}', $ok = true, $sent;
    public function sendRequest($url, $body, $method) {
        self::$sent = ['url'=>$url, 'body'=>json_decode($body,true), 'raw_body'=>$body, 'method'=>$method,
            'headers'=>$this->headers, 'max_redirect'=>$this->max_redirect, 'debug'=>$this->debug];
        $this->status = self::$code; $this->resp_body = self::$body;
        return self::$ok;
    }
}
}
namespace {
define('DOKU_INC', '/unused/');
require '/config/dokuwiki/lib/plugins/petar/PetarClient.php';
`

func jobsID(i int) string { return fmt.Sprintf("019b2c5d-1234-7000-8000-%012x", i) }

func jobsFixture(n int) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, n)
	for i := n; i > 0; i-- {
		rows = append(rows, map[string]interface{}{
			"id": jobsID(i), "process_id": jobsID(500), "tool_name": "<b>tool</b>",
			"status": "completed", "created_at": "2026-11-18T12:30:00.123456789Z",
			"updated_at": "2026-11-18T13:35:00+01:00", "payload": "SECRET_PAYLOAD",
			"priority": 987654, "result_path": "SECRET_PATH", "error_msg": "SECRET_ERR", "extra": "SECRET_EXTRA",
		})
	}
	return rows
}

func TestJobsClient(t *testing.T) {
	valid, _ := json.Marshal(jobsFixture(1))
	for _, tc := range []struct {
		name, body, secret string
		code               int
		ok, success        bool
	}{
		{"valid", string(valid), "test-key", 200, true, true},
		{"empty", "[]", "", 200, true, true},
		{"object", "{}", "", 200, true, false},
		{"scalar", "null", "", 200, true, false},
		{"rowArray", "[[]]", "", 200, true, false},
		{"partial", strings.TrimSuffix(string(valid), "]") + ",{}]", "", 200, true, false},
		{"badID", strings.Replace(string(valid), jobsID(1), "wrong", 1), "", 200, true, false},
		{"badProcess", strings.Replace(string(valid), `"`+jobsID(500)+`"`, `42`, 1), "", 200, true, false},
		{"badTool", strings.Replace(string(valid), `"\u003cb\u003etool\u003c/b\u003e"`, `""`, 1), "", 200, true, false},
		{"badStatus", strings.Replace(string(valid), "completed", "unknown", 1), "", 200, true, false},
		{"badDate", strings.Replace(string(valid), "11-18", "02-30", 1), "", 200, true, false},
		{"broken", "SECRET_RAW", "", 200, true, false},
		{"redirect", string(valid), "", 302, true, false},
		{"httpError", "SECRET_RAW", "", 500, true, false},
		{"transport", "SECRET_RAW", "", 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]interface{}{"body": tc.body, "code": tc.code, "ok": tc.ok, "secret": tc.secret})
			got := runPHP(t, clientFixture+`
$a = json_decode(base64_decode('`+base64Fixture(args)+`'), true);
\dokuwiki\HTTP\DokuHTTPClient::$body = $a['body'];
\dokuwiki\HTTP\DokuHTTPClient::$code = $a['code'];
\dokuwiki\HTTP\DokuHTTPClient::$ok = $a['ok'];
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend', $a['secret']);
try { $result = $c->listJobs('wiki', 'completed', '`+jobsID(2)+`'); $code = 200; }
catch (\RuntimeException $e) { $result = $e->getMessage(); $code = $e->getCode(); }
echo json_encode(['code'=>$code, 'result'=>$result, 'sent'=>\dokuwiki\HTTP\DokuHTTPClient::$sent,
    'logs'=>\dokuwiki\Logger::$messages]);
}`)
			wantCode := float64(502)
			if tc.success {
				wantCode = 200
			}
			raw, _ := json.Marshal(got)
			if got["code"] != wantCode || strings.Contains(string(raw), "SECRET") {
				t.Fatalf("unexpected result or leak: %s", raw)
			}
			sent := got["sent"].(map[string]interface{})
			headers := sent["headers"].(map[string]interface{})
			if sent["url"] != "http://backend/api/v1/jobs?limit=21&status=completed&cursor="+jobsID(2) ||
				sent["raw_body"] != "" || sent["method"] != "GET" || sent["max_redirect"] != float64(0) || sent["debug"] != false ||
				headers["X-Wiki-User"] != "wiki" || (tc.secret == "" && headers["X-API-Key"] != nil) ||
				(tc.secret != "" && headers["X-API-Key"] != tc.secret) {
				t.Fatalf("unexpected upstream request: %#v", sent)
			}
		})
	}
}

func TestUniversalPayload(t *testing.T) {
	for _, data := range []string{
		`{"message":" hello\n"}`, `{"url":"https://example.org/video"}`,
		`{"text":" text\n","pattern":"summarize"}`, `[]`, `{}`, `null`, `"hello"`,
	} {
		t.Run(data, func(t *testing.T) {
			got := runPHP(t, clientFixture+`
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend');
$c->submitAction('fixture', 1, base64_decode('`+base64Fixture([]byte(data))+`'), 'wiki', '`+directRequestID+`');
echo json_encode(\dokuwiki\HTTP\DokuHTTPClient::$sent);
}`)
			want := `{"action":"fixture","version":1,"data":` + data + `}`
			if got["raw_body"] != want {
				t.Fatalf("payload %v, want %s", got["raw_body"], want)
			}
			headers := got["headers"].(map[string]interface{})
			if _, ok := headers["X-API-Key"]; ok {
				t.Fatal("empty API key sent")
			}
		})
	}
}

func TestClientRejectsInvalidInput(t *testing.T) {
	for _, args := range []string{
		`[" ",1,"{}","wiki"]`, `["echo-test",1,"","wiki"]`,
		`["new",0,"null","wiki"]`, `["new",1,"{}{}","wiki"]`,
		`["new",1,"{}",""]`, `["new",1,"{}","wiki","bad-id"]`,
	} {
		t.Run(args, func(t *testing.T) {
			got := runPHP(t, clientFixture+`
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend');
try { $c->submitAction(...json_decode('`+args+`',true)); echo '{"rejected":false}'; }
catch (\InvalidArgumentException $e) { echo '{"rejected":true}'; }
}`)
			if got["rejected"] != true {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestClientRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct{ code, body string }{
		{"200", `{"input_id":"x","process_id":"y","status":"new"}`},
		{"201", `{`}, {"201", `[]`}, {"201", `{"input_id":[],"process_id":123,"status":"new"}`},
		{"201", `{"input_id":"not-a-uuid","process_id":"also-not-a-uuid","status":"new"}`},
		{"201", `{"input_id":"019b2c5d-1234-7000-8000-123456789abc","process_id":"019b2c5d-1234-7000-8000-123456789abd","status":"completed"}`},
		{"500", `{"error":"SECRET"}`},
	} {
		t.Run(tc.code+tc.body, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			got := runPHP(t, clientFixture+`
\dokuwiki\HTTP\DokuHTTPClient::$code = `+tc.code+`;
\dokuwiki\HTTP\DokuHTTPClient::$body = `+string(body)+`;
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend');
try { $c->submitAction('echo-test',1,'{}','wiki'); echo '{"rejected":false}'; }
catch (\RuntimeException $e) { echo json_encode(['rejected'=>true,'code'=>$e->getCode(),'error'=>$e->getMessage()]); }
}`)
			if got["rejected"] != true || got["code"] != float64(502) || strings.Contains(got["error"].(string), "SECRET") {
				t.Fatalf("bad response handling: %#v", got)
			}
		})
	}
}

func TestClientTransportAndSecret(t *testing.T) {
	got := runPHP(t, clientFixture+`
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend', 'test-key');
$c->submitAction('echo-test', 1, '{}', 'wiki');
$headers = \dokuwiki\HTTP\DokuHTTPClient::$sent['headers'];
\dokuwiki\HTTP\DokuHTTPClient::$ok = false;
\dokuwiki\HTTP\DokuHTTPClient::$code = 0;
try { $c->submitAction('echo-test', 1, '{}', 'wiki'); echo '{"rejected":false}'; }
catch (\RuntimeException $e) { echo json_encode(['rejected'=>true,'code'=>$e->getCode(),'headers'=>$headers]); }
}`)
	if got["rejected"] != true || got["code"] != float64(502) ||
		got["headers"].(map[string]interface{})["X-API-Key"] != "test-key" {
		t.Fatalf("transport/API key: %#v", got)
	}
}

func TestReadinessRejectsMalformedRequestID(t *testing.T) {
	got := runPHP(t, clientFixture+`
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend');
try { $c->checkReadiness("12345678-1234-4234-8234-123456789abc\n"); echo '{"rejected":false}'; }
catch (\InvalidArgumentException $e) { echo '{"rejected":true}'; }
}`)
	if got["rejected"] != true {
		t.Fatal("request ID with trailing newline accepted")
	}
}

const (
	directRequestID = "12345678-1234-4234-8234-123456789abc"
	directInputID   = "019b2c5d-1234-7000-8000-123456789abc"
	directProcessID = "019b2c5d-1234-7000-8000-123456789abd"
)

type directBFFResponse struct {
	Requests []string
	Code     int
	Error    string
	Result   struct {
		Success   bool
		RequestID string `json:"request_id"`
		Data      struct {
			Status    string
			InputID   string `json:"input_id"`
			ProcessID string `json:"process_id"`
		}
	}
}

func runDirectBFF(t *testing.T, mode string) directBFFResponse {
	t.Helper()
	const router = `<?php
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
if ($path === '/health') {
    echo basename(__DIR__);
    return;
}
file_put_contents(__DIR__ . '/requests', $_SERVER['REQUEST_METHOD'] . " $path\n", FILE_APPEND);
header('Content-Type: application/json');
if ($path === '/readyz' && getenv('mode') === 'ready_redirect') {
    header('Location: /ready-target', true, 302);
    return;
}
if ($path === '/readyz' || $path === '/ready-target') {
    echo '{"status":"ready"}';
    return;
}
if ($path === '/api/v1/actions' && getenv('mode') === 'submit_redirect') {
    header('Location: /submit-target', true, 307);
    echo '{"error":"PRIVATE_UPSTREAM_BODY"}';
    return;
}
if ($path === '/api/v1/actions' && getenv('mode') === 'submit_rejected') {
    http_response_code(400);
    echo '{"error":"PRIVATE_UPSTREAM_BODY"}';
    return;
}
if ($path === '/api/v1/actions' || $path === '/submit-target') {
    http_response_code(201);
    echo '{"input_id":"` + directInputID + `","process_id":"` + directProcessID + `","status":"new"}';
    return;
}
http_response_code(404);
`
	const caller = `<?php
define('NOSESSION', true);
define('DOKU_INC', '/config/dokuwiki/');
require DOKU_INC . 'inc/init.php';
require_once DOKU_INC . 'lib/plugins/petar/PetarClient.php';
$conf['proxy']['host'] = '';
$conf['allowdebug'] = false;
$conf['dontlog'] = 'error';
$client = new \dokuwiki\plugin\petar\PetarClient($argv[1], '', 2);
try {
    $result = str_starts_with(getenv('mode'), 'submit_')
        ? $client->submitAction('echo-test', 1, '{}', 'wiki', '` + directRequestID + `')
        : $client->checkReadiness('` + directRequestID + `');
    $out = ['result'=>$result, 'code'=>$result['http_status']];
} catch (\RuntimeException | \InvalidArgumentException $e) {
    $out = ['code'=>$e->getCode(), 'error'=>$e->getMessage()];
}
$out['requests'] = file(__DIR__ . '/requests', FILE_IGNORE_NEW_LINES);
echo json_encode($out);
`
	script := `set -eu
dir=$(mktemp -d)
pid=''
cleanup() {
    if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
    rm -rf "$dir"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
printf '%s' "$1" > "$dir/router.php"
printf '%s' "$2" > "$dir/caller.php"
port=$(php -r '$s = stream_socket_server("tcp://127.0.0.1:0"); if (!$s) exit(1); echo substr(strrchr(stream_socket_get_name($s, false), ":"), 1);')
url="http://127.0.0.1:$port"
php -S "127.0.0.1:$port" "$dir/router.php" >"$dir/server.log" 2>&1 &
pid=$!
for i in $(seq 1 20); do
    if [ "$(curl --noproxy '*' -fsS --max-time 1 "$url/health" 2>/dev/null)" = "$(basename "$dir")" ]; then
        php "$dir/caller.php" "$url"
        exit
    fi
    if ! kill -0 "$pid" 2>/dev/null; then break; fi
    sleep 0.1
done
echo 'loopback fixture did not start' >&2
cat "$dir/server.log" >&2
exit 1
`
	cmd := exec.Command("docker", "exec", "-e", "mode="+mode, "--", wikiContainer(), "sh", "-c", script, "fixture", router, caller)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("loopback fixture: %v\n%s", err, out)
	}
	var got directBFFResponse
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("loopback fixture JSON: %v\n%s", err, out)
	}
	return got
}

func TestDirectBFFRequests(t *testing.T) {
	t.Run("submit_rejected", func(t *testing.T) {
		got := runDirectBFF(t, "submit_rejected")
		if strings.Join(got.Requests, "\n") != "POST /api/v1/actions" || got.Result.Success ||
			got.Code != 400 || got.Error != "Input rejected" {
			t.Fatalf("direct HTTP 400 must report a safe rejection after exactly one POST: %#v", got)
		}
	})
	t.Run("ready_redirect", func(t *testing.T) {
		got := runDirectBFF(t, "ready_redirect")
		if strings.Join(got.Requests, "\n") != "GET /readyz" || got.Result.Success || got.Code != 302 {
			t.Fatalf("redirect must not reach its target or report readiness: %#v", got)
		}
	})
	t.Run("submit_redirect", func(t *testing.T) {
		got := runDirectBFF(t, "submit_redirect")
		if strings.Join(got.Requests, "\n") != "POST /api/v1/actions" || got.Result.Success ||
			got.Code != 502 || got.Error != "Backend request failed" {
			t.Fatalf("redirect must not reach its target or report a successful submit: %#v", got)
		}
	})
	t.Run("ready_direct", func(t *testing.T) {
		got := runDirectBFF(t, "ready_direct")
		if strings.Join(got.Requests, "\n") != "GET /readyz" || got.Code != 200 ||
			!got.Result.Success || got.Result.RequestID != directRequestID || got.Result.Data.Status != "ready" {
			t.Fatalf("unexpected direct readiness response: %#v", got)
		}
	})
	t.Run("submit_direct", func(t *testing.T) {
		got := runDirectBFF(t, "submit_direct")
		if strings.Join(got.Requests, "\n") != "POST /api/v1/actions" || got.Code != 201 ||
			!got.Result.Success || got.Result.RequestID != directRequestID || got.Result.Data.Status != "new" ||
			got.Result.Data.InputID != directInputID || got.Result.Data.ProcessID != directProcessID {
			t.Fatalf("unexpected direct submit response: %#v", got)
		}
	})
}
