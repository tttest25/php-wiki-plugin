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
        $plugin->render('xhtml', $renderer, ['mode' => 'jobs']);
        $plugin->render('xhtml', $renderer, ['mode' => 'jobs']);
        $out[$locale] = ['html' => $renderer->doc, 'strings' => $strings,
            'cache' => $renderer->info['cache'], 'tokens' => $tokens];
    }
    foreach (['anonymous', 'ordinary', 'admin'] as $role) {
        $INPUT->server->user = $role === 'anonymous' ? '' : 'alice';
        $USERINFO = ['grps' => [$role === 'ordinary' ? 'user' : ($role === 'admin' ? 'admin' : 'ai')]];
        $tokens = 0;
        $renderer = new Doku_Renderer();
        $plugin->render('xhtml', $renderer, []);
        $plugin->render('xhtml', $renderer, ['mode' => 'jobs']);
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
	cmd := exec.Command("docker", "exec", "-i", "--", wikiContainer(), "php")
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
		if len(regexp.MustCompile(`<select[^>]*name="tool" disabled></select>`).FindAllString(r.HTML, -1)) != 2 {
			t.Errorf("%s: action choices must start empty and disabled", locale)
		}
		if !strings.Contains(r.HTML, `token&quot;&lt;&amp;`) {
			t.Errorf("unsafe token")
		}
	}
	for _, role := range []string{"anonymous", "ordinary"} {
		if r := renders[role]; strings.Contains(r.HTML, "<form") || strings.Contains(r.HTML, "data-petar-jobs") || r.Tokens != 0 {
			t.Errorf("%s: denied render exposes form/token", role)
		}
	}
	if !strings.Contains(renders["admin"].HTML, "<form") {
		t.Error("admin denied")
	}
}

func TestSyntaxJobsMode(t *testing.T) {
	const fixture = `<?php
define('NOSESSION', true);
define('DOKU_INC', '/config/dokuwiki/');
require DOKU_INC . 'inc/init.php';
$out = [];
foreach (['ai', 'admin', 'anonymous', 'ordinary', 'repeated'] as $role) {
    $INPUT->server->set('REMOTE_USER', $role === 'anonymous' ? '' : 'alice');
    $USERINFO = ['grps' => [match ($role) { 'admin' => 'admin', 'ordinary' => 'user', default => 'ai' }]];
    $page = "{{petar}}\n\n{{petar>jobs}}\n";
    if ($role === 'repeated') $page .= "\n{{petar>jobs}}\n\n{{petar>jobsX}}\n";
    $instructions = p_get_instructions($page);
    $count = 0;
    foreach ($instructions as $instruction) {
        if ($instruction[0] === 'plugin' && $instruction[1][0] === 'petar') $count++;
    }
    $info = [];
    $html = p_render('xhtml', $instructions, $info);
    $out[$role] = ['instructions' => $count, 'html' => $html, 'cache' => $info['cache'] ?? null];
}
$out['denial'] = hsc(plugin_load('syntax', 'petar')->getLang('err_forbidden'));
echo json_encode($out);
`
	cmd := exec.Command("docker", "exec", "-i", "--", wikiContainer(), "php")
	cmd.Stdin = strings.NewReader(fixture)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("DokuWiki parser fixture: %v\n%s", err, out)
	}
	type page struct {
		Instructions int
		HTML         string
		Cache        *bool
	}
	var got struct {
		AI, Admin, Anonymous, Ordinary, Repeated page
		Denial                                   string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("DokuWiki parser fixture JSON: %v\n%s", err, out)
	}
	formPattern := regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`)
	journalPattern := regexp.MustCompile(`(?s)<section\b[^>]*\sdata-petar-jobs(?:="[^"]*")?(?:\s[^>]*)?>.*?</section>`)
	for _, tc := range []struct {
		role string
		view page
		jobs int
	}{
		{"ai", got.AI, 1}, {"admin", got.Admin, 1}, {"anonymous", got.Anonymous, 0},
		{"ordinary", got.Ordinary, 0}, {"repeated", got.Repeated, 2},
	} {
		t.Run(tc.role, func(t *testing.T) {
			view := tc.view
			wantInstructions := 2
			if tc.role == "repeated" {
				wantInstructions = 3
			}
			if view.Cache == nil || *view.Cache || view.Instructions != wantInstructions {
				t.Fatalf("expected no cache and %d instructions, got %d", wantInstructions, view.Instructions)
			}
			forms := formPattern.FindAllStringIndex(view.HTML, -1)
			journals := journalPattern.FindAllStringIndex(view.HTML, -1)
			if regexp.MustCompile(`<p>\s*<(?:section|div)\b`).MatchString(view.HTML) {
				t.Fatal("block widget must not be wrapped in a paragraph")
			}
			if tc.jobs == 0 {
				if got.Denial == "" || strings.Count(view.HTML, got.Denial) != 2 ||
					regexp.MustCompile(`<\s*(form|select|button|input)\b|data-petar-jobs|sectok`).MatchString(view.HTML) {
					t.Fatalf("expected localized denials without controls or token: %s", view.HTML)
				}
				return
			}
			if len(forms) != 1 || len(journals) != tc.jobs || strings.Count(view.HTML, `name="sectok"`) != 1 {
				t.Fatalf("expected one form and %d token-free journals; got %d forms and %d journals", tc.jobs, len(forms), len(journals))
			}
			for _, journal := range journals {
				form := forms[0]
				markup := view.HTML[journal[0]:journal[1]]
				if form[0] < journal[1] && journal[0] < form[1] ||
					strings.Contains(markup, "sectok") {
					t.Fatal("journal must not contain or overlap the submit form/token")
				}
				if !regexp.MustCompile(`class="petar-job-card" hidden aria-labelledby="[^"]+"`).MatchString(markup) ||
					!strings.Contains(markup, `<tbody></tbody>`) {
					t.Fatal("expected a hidden labelled card and no initial rows or result buttons")
				}
			}
			ids := map[string]bool{}
			for _, m := range regexp.MustCompile(`\bid="([^"]+)"`).FindAllStringSubmatch(view.HTML, -1) {
				if ids[m[1]] {
					t.Errorf("duplicate id %s", m[1])
				}
				ids[m[1]] = true
			}
		})
	}
}

func TestUIBehavior(t *testing.T) {
	runUIBehavior(t, "forms")
}

func TestJobsUIBehavior(t *testing.T) {
	runUIBehavior(t, "jobs")
}

func runUIBehavior(t *testing.T, mode string) {
	t.Helper()
	out, _ := renderUI(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join(root, "tests/contract/ui_behavior.js"), filepath.Join(root, "script.js"), mode)
	cmd.Stdin = strings.NewReader(string(out))
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Node behavior: %v\n%s", err, result)
	}
}
