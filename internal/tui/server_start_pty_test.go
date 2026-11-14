//go:build linux

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/servicectl"
)

func TestLocalServerQuestionPrecedesTUIAndOutlives15Seconds(t *testing.T) {
	bin := buildKander(t)
	home, env := isolatedHome(t)
	writeCompleteConfig(t, env)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + listener.Addr().String()
	listener.Close()
	path := configPathFromEnv(t, env)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Server.URL = target
	cfg.Project.Name = "startup-question"
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = (servicectl.Controller{}).Stop(ctx, target)
	})
	// Global arguments keep the harness from binding its own already-running
	// service while still invoking the actual bare-command TUI entrance.
	session := startPTYAt(t, t.TempDir(), bin, env, "--lang", "en")
	if !session.waitForPlain("[Y/n]", 10*time.Second) {
		t.Fatalf("no question: %s", session.text())
	}
	for _, part := range []string{target, filepath.Join(home, ".local", "share", "kander-server"), "server.log", "startup-question-"} {
		if !bytes.Contains([]byte(session.text()), []byte(part)) {
			t.Fatalf("missing storage path %s: %s", part, session.text())
		}
	}
	session.mu.Lock()
	altStarted := bytes.Contains(session.out, []byte("\x1b[?1049h"))
	session.mu.Unlock()
	if altStarted {
		t.Fatal("TUI opened before the startup question")
	}
	time.Sleep(16 * time.Second)
	session.send("\n")
	if !session.waitForPlain("No tasks", 15*time.Second) && !session.waitForPlain("Welcome", time.Second) {
		t.Fatalf("TUI did not open after delayed answer: %s", session.text())
	}
	session.send("q")
	time.Sleep(100 * time.Millisecond)
	session.send("q")
	if err := session.waitExit(5 * time.Second); err != nil {
		t.Fatal(err)
	}
}
