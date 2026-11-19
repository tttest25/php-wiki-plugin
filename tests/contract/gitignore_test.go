package contract

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryIgnoreRules(t *testing.T) {
	content, err := os.ReadFile("../../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	// Isolate matching from global excludes, the live index and existing files.
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), content, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path    string
		ignored bool
	}{
		{".env", true}, {".env.local", true}, {"service.env", true},
		{".env.example", false}, {"conf/.env.example", false},
		{".opencode/tasks/task.md", true}, {".opencode/cache/data", true},
		{".opencode/opencode.json", true}, {".opencode/rules.md", false},
		{"go-pet", true}, {"docs/go-pet", false},
		{"project_bundle_go_pet.txt", true}, {"bundle_go_pet.txt", true},
		{"bundle.py", false}, {"debug.log", true}, {"cpu.prof", true},
		{"cpu.out", true}, {"mem.out", true}, {"coverage.html", true},
		{"file:Zone.Identifier", true}, {".DS_Store", true}, {"Thumbs.db", true},
		{".vscode/settings.json", true}, {".idea/workspace.xml", true},
		{".playwright-mcp/trace.zip", true}, {"docs/.playwright-mcp/example.md", false},
		{"spool/processing/job.json", true}, {"spool/processing/.gitkeep", false},
		{"PetarClient.php", false}, {"script.js", false}, {"style.css", false},
		{"docs/todo.md", false}, {"tests/contract/gitignore_test.go", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			cmd := exec.Command("git", "-c", "core.excludesFile=/dev/null",
				"check-ignore", "--no-index", "--verbose", "--", tc.path)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			ignored := err == nil
			if ignored {
				// Git exits 0 for a matching negation too: inspect the winning rule.
				entry := strings.SplitN(string(out), "\t", 2)
				rule := strings.SplitN(entry[0], ":", 3)
				if len(rule) != 3 {
					t.Fatalf("unexpected check-ignore output: %q", out)
				}
				ignored = !strings.HasPrefix(rule[2], "!")
			}
			if err != nil {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
					t.Fatalf("check-ignore: %v\n%s", err, out)
				}
			}
			if ignored != tc.ignored {
				t.Errorf("ignored=%v, want %v", ignored, tc.ignored)
			}
		})
	}
}

func TestIgnoreRulesHaveNoDuplicates(t *testing.T) {
	content, err := os.ReadFile("../../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			t.Errorf("duplicate ignore rule: %s", line)
		}
		seen[line] = true
	}
}
