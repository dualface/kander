//go:build !windows

package terminalcheck

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal/builtin"
)

func TestEmbeddedTmuxConformance(t *testing.T) {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	root := t.TempDir()
	socket := filepath.Join(root, "socket")
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	command := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, tmux, append([]string{"-S", socket}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("isolated tmux %q: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, tmux, "-S", socket, "kill-server").Run()
	})
	pane := command("-f", "/dev/null", "new-session", "-d", "-s", "conformance", "-P", "-F", "#{pane_id}", "/bin/sh")
	// Every Backend invocation, including Prepare, uses this private socket.
	wrapper := filepath.Join(root, "tmux")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+quote(tmux)+" -S "+quote(socket)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", fmt.Sprintf("%s,%s,0", socket, command("display-message", "-p", "-t", pane, "#{pid}")))
	t.Setenv("TMUX_PANE", pane)
	attached := attachIsolatedClient(t, tmux, socket)
	if attached {
		deadline := time.Now().Add(5 * time.Second)
		for command("list-clients", "-F", "#{client_tty}") == "" {
			if time.Now().After(deadline) {
				t.Fatal("PTY client did not attach")
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	backend, err := builtin.DefinitionBackend("tmux", "tmux", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Check(backend, Options{}, &output)
	t.Log("embedded tmux definition:\n" + output.String())
	if err != nil {
		t.Error(err)
	}
	want := "skip Focus"
	if attached {
		want = "pass Focus"
	}
	if !strings.Contains(output.String(), want) {
		t.Fatalf("expected %s", want)
	}
	// The permanent seed pane must survive the check's container cleanup.
	if got := command("list-panes", "-a", "-F", "#{pane_id}"); got != pane {
		t.Fatalf("unexpected surviving panes: %q", got)
	}
	if attached {
		for _, client := range strings.Fields(command("list-clients", "-F", "#{client_tty}")) {
			command("detach-client", "-t", client)
		}
		deadline := time.Now().Add(5 * time.Second)
		for command("list-clients", "-F", "#{client_tty}") != "" {
			if time.Now().After(deadline) {
				t.Fatal("client did not detach")
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	output.Reset()
	if err := Check(backend, Options{}, &output); err != nil {
		t.Errorf("no-client run: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "skip Focus") || !strings.Contains(output.String(), "no attached terminal client") {
		t.Fatalf("no-client skip missing\n%s", output.String())
	}
	t.Log("tmux no-client check:\n" + output.String())
}
