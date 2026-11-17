package contract

import (
	"encoding/json"
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
class Logger { public static function error(...$args) {} }
}
namespace dokuwiki\HTTP {
class DokuHTTPClient {
    public $timeout, $status = 201, $resp_body, $error = '', $headers = [];
    public static $code = 201, $body = '{"input_id":"019b2c5d-1234-7000-8000-123456789abc","process_id":"019b2c5d-1234-7000-8000-123456789abd","status":"new"}', $ok = true, $sent;
    public function sendRequest($url, $body, $method) {
        self::$sent = ['url'=>$url, 'body'=>json_decode($body,true), 'headers'=>$this->headers];
        $this->status = self::$code; $this->resp_body = self::$body;
        return self::$ok;
    }
}
}
namespace {
define('DOKU_INC', '/unused/');
require '/config/dokuwiki/lib/plugins/petar/PetarClient.php';
`

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
