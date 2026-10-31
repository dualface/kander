package main

// platform-checks.yml selects tests by name, and `go test -run` exits 0 when its
// pattern matches nothing. A renamed or deleted test therefore leaves a green CI
// step that runs no test. These tests turn that state into a failure.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	repositoryRoot = filepath.Join("..", "..")
	runFlagWord    = regexp.MustCompile(`(^|\s)-run(\s|=|$)`)
)

// workflowRunPattern is one `go test -run` command of a workflow file.
type workflowRunPattern struct {
	line     int
	packages []string
	pattern  string
}

// parseWorkflowRunPatterns needs each command on one line, and refuses a -run it
// cannot attribute: a skipped command would be the silent gap this file closes.
func parseWorkflowRunPatterns(workflow string) ([]workflowRunPattern, error) {
	var found []workflowRunPattern
	line := 0
	for text := range strings.SplitSeq(workflow, "\n") {
		line++
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "#") || !runFlagWord.MatchString(text) {
			continue
		}
		_, command, ok := strings.Cut(text, "go test ")
		if !ok {
			return nil, fmt.Errorf("line %d: -run outside a one-line go test command", line)
		}
		current := workflowRunPattern{line: line}
		words := strings.Fields(command)
		for i, word := range words {
			if strings.HasPrefix(word, "./") {
				current.packages = append(current.packages, word)
			} else if value, ok := strings.CutPrefix(word, "-run="); ok {
				current.pattern = strings.Trim(value, `"'`)
			} else if word == "-run" && i+1 < len(words) {
				current.pattern = strings.Trim(words[i+1], `"'`)
			}
		}
		if len(current.packages) == 0 || current.pattern == "" {
			return nil, fmt.Errorf("line %d: go test command without a package path or a -run value", line)
		}
		found = append(found, current)
	}
	return found, nil
}

// countListedTests asks the go command, so the count follows the same build
// constraints as the CI step on this platform.
func countListedTests(t *testing.T, root string, p workflowRunPattern) (int, error) {
	t.Helper()
	args := append([]string{"test", "-list", p.pattern}, p.packages...)
	command := exec.CommandContext(t.Context(), "go", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	count := 0
	for text := range strings.Lines(string(output)) {
		// -run never selects a benchmark, so -list's Benchmark lines do not count.
		if strings.HasPrefix(text, "Test") || strings.HasPrefix(text, "Fuzz") || strings.HasPrefix(text, "Example") {
			count++
		}
	}
	return count, nil
}

func checkWorkflowRunPatterns(t *testing.T, root, workflow string) error {
	t.Helper()
	patterns, err := parseWorkflowRunPatterns(workflow)
	if err != nil {
		return err
	}
	if len(patterns) == 0 {
		return errors.New("no go test -run command found")
	}
	var problems []error
	for _, p := range patterns {
		count, err := countListedTests(t, root, p)
		switch {
		case err != nil:
			problems = append(problems, fmt.Errorf("line %d: %w", p.line, err))
		case count == 0:
			problems = append(problems, fmt.Errorf("line %d: -run %q matches no test in %s", p.line, p.pattern, strings.Join(p.packages, " ")))
		default:
			t.Logf("line %d: -run %q matches %d tests", p.line, p.pattern, count)
		}
	}
	return errors.Join(problems...)
}

func TestWorkflowRunPatternsMatchTests(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join(repositoryRoot, ".github", "workflows", "platform-checks.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkWorkflowRunPatterns(t, repositoryRoot, string(workflow)); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowRunPatternChecksReportFailures(t *testing.T) {
	const existing = "TestWorkflowRunPatternChecksReportFailures"
	cases := []struct {
		name     string
		workflow string
		want     string
	}{
		{"missing test", "run: go test ./cmd/kander -run TestNoSuchTest\n", `line 1: -run "TestNoSuchTest" matches no test in ./cmd/kander`},
		{"one missing of two", "run: go test ./cmd/kander -run " + existing + "\n  go test ./cmd/kander -run=\"TestNoSuchTest\"\n", `line 2: -run "TestNoSuchTest" matches no test`},
		{"unknown package", "run: go test ./cmd/no-such-package -run TestCheck\n", "line 1: go test -list TestCheck ./cmd/no-such-package"},
		{"no command", "run: go build ./...\n# go test ./cmd/kander -run TestCheck\n", "no go test -run command found"},
		{"continued command", "run: go test ./cmd/kander \\\n  -run TestCheck\n", "line 2: -run outside a one-line go test command"},
		{"no package path", "run: go test -run TestCheck\n", "line 1: go test command without a package path"},
		{"no pattern", "run: go test ./cmd/kander -run\n", "line 1: go test command without a package path or a -run value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkWorkflowRunPatterns(t, repositoryRoot, c.workflow)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v, want %q", err, c.want)
			}
			if strings.Contains(err.Error(), existing) {
				t.Fatalf("the existing test was reported: %v", err)
			}
		})
	}
}
