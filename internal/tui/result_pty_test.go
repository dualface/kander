//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/issue"
)

func TestIssueResultDialogOnPTY(t *testing.T) {
	bin := buildKander(t)
	root, env := boardEnv(t)
	writeCompleteConfig(t, env)
	writeFakeIssueCommands(t, env)
	repository := issue.Repository{Host: "github.com", Owner: "dualface", Name: "kander", URL: "https://github.com/dualface/kander"}
	at := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
	record, err := issue.BuildImportSnapshot(issue.IssueSnapshot{Repository: repository, Number: 42, Title: "Completed", State: "open", CreatedAt: at, UpdatedAt: at, FetchedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	data, err := issue.MarshalImportSnapshot(record)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "done", "20260914-result-task")
	if err := os.MkdirAll(filepath.Join(dir, "source"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, issue.SourceFileName), data, 0600); err != nil {
		t.Fatal(err)
	}
	spec := "# Completed\n\n- SIZE: small\n- LANGUAGE: en\n\n## SUMMARY\n\nResolved.\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	env = append(env, "TMUX=/tmp/pty-tmux,1,0", "TMUX_PANE=%419")
	session := startPTY(t, bin, env)
	if !session.waitFor("Task Board", 8*time.Second) {
		t.Fatal(session.text())
	}
	session.send("g")
	if !session.waitForPlain("PTY issue title", 10*time.Second) {
		t.Fatal(session.text())
	}
	if !session.waitForPlain("reconcile result", 10*time.Second) {
		t.Fatal(session.text())
	}
	session.send("s")
	if !session.waitForPlain("Reconcile result of issue #42", 10*time.Second) {
		t.Fatal(session.text())
	}
	if !session.waitForPlain("Closing requires", 10*time.Second) || !session.waitForPlain("separate explicit confirmation", 10*time.Second) {
		t.Fatal(session.text())
	}
	session.send("\x1b")
	time.Sleep(300 * time.Millisecond)
	session.send("\x1b")
	time.Sleep(300 * time.Millisecond)
	session.send("q")
	if err := session.waitExit(8 * time.Second); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "spec.md"))
	if string(after) != spec {
		t.Fatal("result confirmation changed card")
	}
	if strings.Contains(session.text(), "panic:") {
		t.Fatal(session.text())
	}
}
