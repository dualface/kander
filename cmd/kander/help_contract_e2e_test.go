package main

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

// Every command answers every form of help request with the contract usage of its top-level
// command: the Syntax lines, the contract path, and the conditional read sentence, on stdout
// with exit 0.
func TestEveryCommandHelpPrintsContractUsage(t *testing.T) {
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		if leaf.Hidden {
			continue
		}
		path := strings.Fields(leaf.path)
		contract := path[0]
		want := config.CommandUsage(contract) + "\n"
		// The request forms are documented in the notify contract.
		file := contract
		if contract == "request" {
			file = "notify"
		}
		for name, args := range map[string][]string{
			"--help": append(append([]string{}, path...), "--help"),
			"help":   append([]string{"help"}, path...),
			"-h":     append(append([]string{}, path...), "-h"),
		} {
			t.Run(leaf.path+"/"+name, func(t *testing.T) {
				result := runKander(t, args...)
				// An installation that predates the contracts gets the once-per-file hint to run
				// kander doctor on stderr; nothing else may appear there.
				if result.stderr != "" && !strings.Contains(result.stderr, "kander doctor") {
					t.Fatalf("unexpected stderr: %q", result.stderr)
				}
				if result.code != 0 || result.stdout != want {
					t.Fatalf("exit=%d\nstdout=%q\nwant=%q", result.code, result.stdout, want)
				}
				lines := strings.Split(strings.TrimRight(result.stdout, "\n"), "\n")
				if len(lines) < 3 || !strings.HasPrefix(lines[0], "kander "+contract) || !strings.HasPrefix(lines[len(lines)-2], "Contract: ") || !strings.HasSuffix(lines[len(lines)-2], "/snippets/commands/"+file+".md") {
					t.Fatalf("usage shape:\n%s", result.stdout)
				}
				if lines[len(lines)-1] != config.CommandReadSentence(strings.TrimSuffix(strings.TrimPrefix(lines[len(lines)-2], "Contract: "), "/snippets/commands/"+file+".md")) {
					t.Fatalf("last line is not the read sentence:\n%s", result.stdout)
				}
			})
		}
	}
}

// The top-level help is the usage text, and the help of an unknown topic is an argument error.
func TestTopLevelHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"-h"}} {
		result := runKander(t, args...)
		if result.code != 0 || !strings.HasPrefix(result.stdout, "usage: kander") || !strings.Contains(result.stdout, "  list / ls\n") || strings.Contains(result.stdout, "worker") {
			t.Fatalf("%v: exit=%d stdout=%q", args, result.code, result.stdout)
		}
	}
	if result := runKander(t, "help", "nosuchcommand"); result.code != 2 {
		t.Fatalf("unknown help topic: exit=%d stderr=%q", result.code, result.stderr)
	}
}
