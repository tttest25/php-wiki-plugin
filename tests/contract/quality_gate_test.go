package contract

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQualityGate(t *testing.T) {
	source, err := os.ReadFile("../../scripts/quality-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	const plugin = "/config/dokuwiki/lib/plugins/petar"
	for _, tc := range []struct {
		name, fail, wantLast string
	}{
		{"success", "", "go test -count=1 -race -timeout 120s ./tests/contract"},
		{"isolatedContainer", "", "go test -count=1 -race -timeout 120s ./tests/contract"},
		{"isolatedWrongCheckout", "mount", "docker inspect"},
		{"wrongCheckout", "mount", "docker inspect"},
		{"phpFailure", "docker exec", "docker exec dokuwiki php -l " + plugin + "/nested/file with spaces.php"},
		{"nodeFailure", "node", "node --check -- nested/fixture.js"},
		{"vetFailure", "go vet", "go vet ./tests/contract"},
		{"raceFailure", "go test", "go test -count=1 -race -timeout 120s ./tests/contract"},
		{"unstagedWhitespace", "unstaged", ""},
		{"stagedWhitespace", "staged", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := "dokuwiki"
			override := ""
			if strings.HasPrefix(tc.name, "isolated") {
				container, override = "php03-fixture", "php03-fixture"
			}
			parent := t.TempDir()
			root := filepath.Join(parent, "checkout")
			write := func(path string, content []byte, mode os.FileMode) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, content, mode); err != nil {
					t.Fatal(err)
				}
			}
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			gate := filepath.Join(root, "scripts", "quality-gate.sh")
			write(gate, source, 0755)
			write(filepath.Join(root, "nested/file with spaces.php"), []byte("<?php\n"), 0644)
			write(filepath.Join(root, "nested/fixture.js"), []byte("// fixture\n"), 0644)
			git("init", "-q")
			git("add", ".")
			git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture")
			if tc.fail == "staged" || tc.fail == "unstaged" {
				write(filepath.Join(root, "nested/fixture.js"), []byte("// trailing space \n"), 0644)
				if tc.fail == "staged" {
					git("add", "nested/fixture.js")
				}
			}
			const stub = `#!/bin/sh
set -eu
name=${0##*/}
[ "$PWD" = "$GATE_ROOT" ]
if [ "$name $1" = "docker inspect" ]; then
    for target do :; done
    printf 'docker inspect %s\n' "$target" >> "$GATE_LOG"
    if [ "$GATE_FAIL" = mount ]; then echo 'bind /another/checkout'; else printf 'bind %s\n' "$GATE_ROOT"; fi
    exit 0
fi
if [ "$name $1" = "docker exec" ]; then
    if [ "$2" = -- ]; then
        [ "$#" = 6 ]
        shift
        set -- exec "$2" "$3" "$4" "$5"
    fi
    [ "$#" = 5 ]
fi
printf '%s %s\n' "$name" "$*" >> "$GATE_LOG"
if [ "$name" = go ]; then [ "$GO111MODULE" = off ]; fi
if [ "$GATE_FAIL" = "$name" ] || [ "$GATE_FAIL" = "$name $1" ]; then exit 42; fi
`
			bin, log := filepath.Join(parent, "bin"), filepath.Join(parent, "commands.log")
			for _, name := range []string{"docker", "node", "go"} {
				write(filepath.Join(bin, name), []byte(stub), 0755)
			}
			write(log, nil, 0644)
			cmd := exec.Command(gate)
			cmd.Dir = parent
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "GO111MODULE=on",
				"GATE_ROOT="+root, "GATE_LOG="+log, "GATE_FAIL="+tc.fail, "PETAR_TEST_WIKI_CONTAINER="+override)
			out, err := cmd.CombinedOutput()
			pass := bytes.Contains(out, []byte("QUALITY GATE PASSED"))
			if (err == nil) != (tc.fail == "") || pass != (tc.fail == "") {
				t.Fatalf("unexpected gate result: %v\n%s", err, out)
			}
			if tc.fail == "docker exec" || tc.fail == "node" || strings.HasPrefix(tc.fail, "go ") {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 42 {
					t.Fatalf("expected propagated exit 42, got %v", err)
				}
			}
			trace, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"docker inspect " + container, "docker exec " + container + " php -l " + plugin + "/nested/file with spaces.php",
				"node --check -- nested/fixture.js", "go vet ./tests/contract", "go test -count=1 -race -timeout 120s ./tests/contract",
			}
			last := tc.wantLast
			if last == "docker inspect" {
				last += " " + container
			}
			i := 0
			for i < len(want) && want[i] != last {
				i++
			}
			expected := ""
			if tc.wantLast != "" {
				if i == len(want) {
					t.Fatalf("unknown expected last command %q", tc.wantLast)
				}
				expected = strings.Join(want[:i+1], "\n") + "\n"
			}
			if string(trace) != expected {
				t.Fatalf("command trace:\n%s\nwant:\n%s", trace, expected)
			}
		})
	}
}
