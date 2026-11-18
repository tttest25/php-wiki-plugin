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
	cmd := exec.Command("docker", "exec", "-i", "dokuwiki", "php")
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

func TestCanonicalPayload(t *testing.T) {
	for _, tc := range []struct{ tool, message, pattern, want string }{
		{"echo-test", " hello\n", "", `{"tool":"echo-test","message":" hello\n"}`},
		{"yt-dlp", "https://example.org/video", "", `{"tool":"yt-dlp","url":"https://example.org/video"}`},
		{"fabric", " text\n", "", `{"tool":"fabric","text":" text\n","pattern":"summarize"}`},
		{"fabric", "text", "extract_wisdom", `{"tool":"fabric","text":"text","pattern":"extract_wisdom"}`},
	} {
		t.Run(tc.tool+tc.pattern, func(t *testing.T) {
			args, _ := json.Marshal([]string{tc.tool, tc.message, "wiki", "12345678-1234-4234-8234-123456789abc"})
			extra := ""
			if tc.pattern != "" {
				pattern, _ := json.Marshal(tc.pattern)
				extra = "$args[] = " + string(pattern) + ";"
			}
			got := runPHP(t, clientFixture+`
$args = json_decode('`+string(args)+`', true); `+extra+`
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend');
$result = $c->submitInput(...$args);
echo json_encode(\dokuwiki\HTTP\DokuHTTPClient::$sent);
}`)
			payload := got["body"].(map[string]interface{})["payload"]
			actual, _ := json.Marshal(payload)
			var want interface{}
			json.Unmarshal([]byte(tc.want), &want)
			expected, _ := json.Marshal(want)
			if string(actual) != string(expected) {
				t.Fatalf("payload %s, want %s", actual, expected)
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
		`["unknown","text","wiki"]`, `["echo-test"," \n","wiki"]`,
		`["yt-dlp","ftp://example.org","wiki"]`, `["yt-dlp","https:///path","wiki"]`,
		`["fabric","text","wiki",null,""]`, `["fabric","text","wiki",null,"../bad"]`,
	} {
		t.Run(args, func(t *testing.T) {
			got := runPHP(t, clientFixture+`
$c = new \dokuwiki\plugin\petar\PetarClient('http://backend');
try { $c->submitInput(...json_decode('`+args+`',true)); echo '{"rejected":false}'; }
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
try { $c->submitInput('echo-test','hello','wiki'); echo '{"rejected":false}'; }
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
$c->submitInput('echo-test', 'hello', 'wiki');
$headers = \dokuwiki\HTTP\DokuHTTPClient::$sent['headers'];
\dokuwiki\HTTP\DokuHTTPClient::$ok = false;
\dokuwiki\HTTP\DokuHTTPClient::$code = 0;
try { $c->submitInput('echo-test', 'hello', 'wiki'); echo '{"rejected":false}'; }
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
