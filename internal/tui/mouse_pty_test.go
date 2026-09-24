//go:build linux

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestStatusMouseOnPTY(t *testing.T) {
	bin := buildKander(t)
	_, env := boardEnv(t)
	writeCompleteConfig(t, env)
	session := startPTY(t, bin, env)
	if !session.waitFor("o options | ? help", 8*time.Second) {
		t.Fatal("status actions missing")
	}
	// Dismiss the empty-board welcome before clicking the underlying status.
	session.send("\x1b")
	time.Sleep(300 * time.Millisecond)
	session.send("\x1b[<0;116;32M\x1b[<0;116;32m")
	if !session.waitFor("Key bindings", 8*time.Second) {
		t.Fatalf("mouse did not open help: %s", ansi.Strip(session.text()))
	}
	before := session.size()
	session.send(strings.Repeat("\x1b[<65;50;15M", 20))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(ansi.Strip(session.textFrom(before)), "cards / document") {
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(ansi.Strip(session.textFrom(before)), "cards / document") {
		t.Fatal("help wheel did not reveal final group")
	}
	session.send("\x1b[<0;5;5M\x1b[<0;5;5m")
	time.Sleep(100 * time.Millisecond)
	session.send("\x1b[<0;104;32M\x1b[<0;104;32m")
	if !session.waitFor("Review and models", 10*time.Second) {
		t.Fatal("mouse did not close help and open options")
	}
	session.send("\x1b")
	time.Sleep(300 * time.Millisecond)
	session.send("q")
	if err := session.waitExit(8 * time.Second); err != nil {
		t.Fatal(err)
	}
}
