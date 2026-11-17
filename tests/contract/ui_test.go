package contract

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Exercise the real renderer, not a reimplementation of its markup.
const uiPHP = `<?php
namespace dokuwiki\Extension {
    class SyntaxPlugin {
        public function getLang($key) { return $GLOBALS['strings'][$key] ?? $key; }
    }
}
namespace dokuwiki\Parsing { class ModeRegistry { const CATEGORY_SUBSTITUTION = 'substition'; } }
namespace {
    define('DOKU_INC', '/config/dokuwiki/');
    class Doku_Renderer { public $doc = ''; public $info = []; }
    function hsc($s) { return htmlspecialchars($s, ENT_QUOTES, 'UTF-8'); }
    function getSecurityToken() { $GLOBALS['tokens']++; return 'token"<&'; }
    class ServerInput { public $user = 'alice'; function str($key) { return $this->user; } }
    $INPUT = (object)['server' => new ServerInput()];
    $_SERVER['REMOTE_USER'] = 'spoofed';
    require '/config/dokuwiki/lib/plugins/petar/syntax.php';
    $out = [];
    foreach (['en', 'ru'] as $locale) {
        $lang = [];
        require '/config/dokuwiki/lib/plugins/petar/lang/' . $locale . '/lang.php';
        $strings = $lang;
        $USERINFO = ['grps' => ['ai']];
        $tokens = 0;
        $renderer = new Doku_Renderer();
        $plugin = new syntax_plugin_petar();
        $plugin->render('xhtml', $renderer, []);
        $plugin->render('xhtml', $renderer, []);
        $out[$locale] = ['html' => $renderer->doc, 'strings' => $strings,
            'cache' => $renderer->info['cache'], 'tokens' => $tokens];
    }
    foreach (['anonymous', 'ordinary', 'admin'] as $role) {
        $INPUT->server->user = $role === 'anonymous' ? '' : 'alice';
        $USERINFO = ['grps' => [$role === 'ordinary' ? 'user' : ($role === 'admin' ? 'admin' : 'ai')]];
        $tokens = 0;
        $renderer = new Doku_Renderer();
        $plugin->render('xhtml', $renderer, []);
        $out[$role] = ['html' => $renderer->doc, 'tokens' => $tokens];
    }
    echo json_encode($out);
}
`

type uiRender struct {
	HTML   string `json:"html"`
	Cache  bool   `json:"cache"`
	Tokens int    `json:"tokens"`
}

func renderUI(t *testing.T) ([]byte, map[string]uiRender) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", "dokuwiki", "php")
	cmd.Stdin = strings.NewReader(uiPHP)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PHP UI fixture: %v\n%s", err, out)
	}
	var renders map[string]uiRender
	if err := json.Unmarshal(out, &renders); err != nil {
		t.Fatalf("fixture JSON: %v\n%s", err, out)
	}
	return out, renders
}

func TestUIRenderer(t *testing.T) {
	_, renders := renderUI(t)
	for _, locale := range []string{"en", "ru"} {
		r := renders[locale]
		if r.Cache || r.Tokens != 2 {
			t.Errorf("%s: cache/token contract: %+v", locale, r)
		}
		ids := map[string]bool{}
		for _, m := range regexp.MustCompile(`\bid="([^"]+)"`).FindAllStringSubmatch(r.HTML, -1) {
			if ids[m[1]] {
				t.Errorf("%s: duplicate id %s", locale, m[1])
			}
			ids[m[1]] = true
		}
		for _, m := range regexp.MustCompile(`\bfor="([^"]+)"`).FindAllStringSubmatch(r.HTML, -1) {
			if !ids[m[1]] {
				t.Errorf("orphan label %s", m[1])
			}
		}
		if len(regexp.MustCompile(`<button[^>]*type="submit"[^>]*disabled`).FindAllString(r.HTML, -1)) != 2 {
			t.Errorf("%s: both submit buttons must start disabled", locale)
		}
		if !strings.Contains(r.HTML, `name="pattern"`) {
			t.Errorf("%s: missing pattern", locale)
		}
		if !strings.Contains(r.HTML, `token&quot;&lt;&amp;`) {
			t.Errorf("unsafe token")
		}
	}
	for _, role := range []string{"anonymous", "ordinary"} {
		if r := renders[role]; strings.Contains(r.HTML, "<form") || r.Tokens != 0 {
			t.Errorf("%s: denied render exposes form/token", role)
		}
	}
	if !strings.Contains(renders["admin"].HTML, "<form") {
		t.Error("admin denied")
	}
}

func TestUIBehavior(t *testing.T) {
	out, _ := renderUI(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join(root, "tests/contract/ui_behavior.js"), filepath.Join(root, "script.js"))
	cmd.Stdin = strings.NewReader(string(out))
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Node behavior: %v\n%s", err, result)
	}
}
