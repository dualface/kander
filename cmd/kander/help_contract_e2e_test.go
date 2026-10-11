package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// Every registered command answers --help with its contract usage: the Syntax lines, the
// contract path, and the conditional read sentence, on stdout with exit 0.
func TestEveryCommandHelpPrintsContractUsage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "cli", "cli.go"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)var commandNames = \[\]string\{(.*?)\n\}`).FindStringSubmatch(string(data))
	if block == nil {
		t.Fatal("commandNames not found")
	}
	for _, m := range regexp.MustCompile(`"([a-z-]+)"`).FindAllStringSubmatch(block[1], -1) {
		cmd := m[1]
		t.Run(cmd, func(t *testing.T) {
			result := runKander(t, cmd, "--help")
			want := config.CommandUsage(cmd) + "\n"
			// An installation that predates the contracts gets the once-per-file hint to run
			// kander doctor on stderr; nothing else may appear there.
			if result.stderr != "" && !strings.Contains(result.stderr, "kander doctor") {
				t.Fatalf("unexpected stderr: %q", result.stderr)
			}
			if result.code != 0 || result.stdout != want {
				t.Fatalf("exit=%d\nstdout=%q\nwant=%q", result.code, result.stdout, want)
			}
			lines := strings.Split(strings.TrimRight(result.stdout, "\n"), "\n")
			if len(lines) < 3 || !strings.HasPrefix(lines[0], "kander "+cmd) || !strings.HasPrefix(lines[len(lines)-2], "Contract: ") || !strings.HasSuffix(lines[len(lines)-2], "/snippets/commands/"+cmd+".md") {
				t.Fatalf("usage shape:\n%s", result.stdout)
			}
			if lines[len(lines)-1] != config.CommandReadSentence(strings.TrimSuffix(strings.TrimPrefix(lines[len(lines)-2], "Contract: "), "/snippets/commands/"+cmd+".md")) {
				t.Fatalf("last line is not the read sentence:\n%s", result.stdout)
			}
		})
	}
}
