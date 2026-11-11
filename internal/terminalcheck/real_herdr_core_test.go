//go:build unix

package terminalcheck

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/builtin"
)

func herdrCoreE2E(t *testing.T, env map[string]string) (terminal.Backend, terminal.Conn) {
	t.Helper()
	if os.Getenv("KANDER_E2E_HERDR_CORE") != "1" {
		t.Skip("herdr-core: set KANDER_E2E_HERDR_CORE=1 explicitly to create a test terminal")
	}
	program, err := exec.LookPath(builtin.HerdrCoreExecutable)
	if err != nil {
		t.Skip("herdr-core: hc is not in PATH")
	}
	// Never connect an opt-in smoke to the user's core or its terminals.
	root, err := os.MkdirTemp("/var/tmp", "kander-hc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for name, value := range map[string]string{
		"HC_SESSION": "kander-test", "HC_SOCKET": filepath.Join(root, "core.sock"),
		"HC_TERMINAL_ID": "", "XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_STATE_HOME": filepath.Join(root, "state"), "XDG_RUNTIME_DIR": root,
	} {
		t.Setenv(name, value)
	}
	t.Cleanup(func() {
		res, err := probe.CaptureContext(context.Background(), program, []string{"server", "stop", "--json"})
		if err != nil || res.Code != 0 {
			t.Errorf("isolated core cleanup: %+v %v", res, err)
		}
	})
	res, err := probe.CaptureContext(t.Context(), program, []string{"server", "start", "--json"})
	if err != nil || res.Code != 0 {
		t.Fatalf("isolated core startup: %+v %v", res, err)
	}
	getenv := func(name string) string {
		if value, ok := env[name]; ok {
			return value
		}
		return os.Getenv(name)
	}
	backend, err := builtin.DefinitionBackend("herdr-core", builtin.HerdrCore, getenv)
	if err != nil {
		t.Fatal(err)
	}
	return backend, terminal.Conn{Program: program, Run: terminal.SpawnRunner}
}

// TestEmbeddedHerdrCoreConformance runs the embedded herdr-core definition
// against an isolated core. It is opt-in because it opens a real terminal.
func TestEmbeddedHerdrCoreConformance(t *testing.T) {
	backend, _ := herdrCoreE2E(t, nil)
	var output bytes.Buffer
	err := Check(backend, Options{SkipFocus: true}, &output)
	t.Log("embedded herdr-core definition:\n" + output.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "pass PaneFacts.gone") {
		t.Fatal("missing gone check")
	}
}

// The environment Kander forwards reaches the process in the hc terminal,
// although the core's own environment does not carry it.
func TestHerdrCoreForwardsKanderEnvironment(t *testing.T) {
	marker := "/kander-e2e/" + time.Now().Format("150405.000000") + "/config.json"
	backend, conn := herdrCoreE2E(t, map[string]string{"KANDER_CONFIG": marker})
	cwd := t.TempDir()
	address, err := backend.CreateContainer(conn, terminal.Target{}, cwd, "kander-env-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.CloseContainer(context.Background(), conn, address) })
	if err := backend.WaitReady(conn, address.Pane, int(terminal.DefaultReadyTimeout/time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	command := `sh -c 'printf "kander-env:%s\n" "$KANDER_CONFIG"; exec cat'`
	if err := backend.RunCommand(conn, address.Pane, "'"+cwd+"'", command, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		output, err := backend.ReadOutput(context.Background(), conn, address.Pane)
		if err == nil && strings.Contains(output, "\nkander-env:"+marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("forwarded KANDER_CONFIG not visible: %q %v", output, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
