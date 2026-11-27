package notify

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dualface/kander/v2/internal/worker"
)

// writeFakeHCPane installs an hc whose every answer is one herdr-core pane
// with the given session marker and an idle Codex program status.
func writeFakeHCPane(t *testing.T, fakeBin, marker string) {
	t.Helper()
	body := `{"address":"t8","labels":{"kander.session":"` + marker + `"},"state":"live","status":{"app":"codex","state":"idle"}}`
	if err := os.WriteFile(filepath.Join(fakeBin, "hc"), []byte("#!/bin/sh\nprintf '%s\\n' '"+body+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The full Codex session a remote start records on the card finds the
// herdr-core pane whose marker is the discovered id; the bare agent name does
// not, because the marker differs from its empty reference.
func TestRequestTargetOfACodexSessionMatchesThePaneMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake hc is a POSIX shell script")
	}
	_, fakeBin := setupBoard(t)
	const id = "01a20fe4-5f0f-7a42-a040-cd6e31cc675c"
	writeFakeHCPane(t, fakeBin, id)
	prompt, err := resolveRequest(worker.AgentAddress{Window: "herdr-core:t8", Session: "codex " + id})
	if err != nil || prompt == nil {
		t.Fatalf("resolve codex <id>: prompt=%v err=%v", prompt != nil, err)
	}
	prompt, err = resolveRequest(worker.AgentAddress{Window: "herdr-core:t8", Session: "codex"})
	if err == nil || prompt != nil {
		t.Fatal("a bare agent session resolved a pane that carries another session marker")
	}
}
