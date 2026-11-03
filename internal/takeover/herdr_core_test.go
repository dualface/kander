package takeover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/launch"
)

// writeFakeHC installs an hc whose terminal t8 runs claude with session s1
// until the agent receives /exit; then the --rm terminal is gone. status
// sets the reported OSC 7501 status of the live terminal.
func writeFakeHC(t *testing.T, fakeBin, root, status string) string {
	t.Helper()
	log := filepath.Join(root, "hc.log")
	exited := filepath.Join(root, "hc.exited")
	live := `{"address":"t8","labels":{"kander-session":"s1"},"state":"live","status":` + status + `}`
	script := `#!/bin/sh
printf '%s\n' "$*" >> ` + log + `
gone() { printf '%s\n' '{"error":{"kind":"not_found","message":"no terminal"}}' >&2; exit 3; }
case "$1" in
get) [ -f ` + exited + ` ] && gone; printf '%s\n' '` + live + `' ;;
ls) if [ -f ` + exited + ` ]; then printf '%s\n' '{"terminals":[]}'; else printf '%s\n' '{"terminals":[{"address":"t8","state":"live"}]}'; fi ;;
send) [ "$4" = /exit ] && : > ` + exited + `; printf '{}\n' ;;
kill|rm) : > ` + exited + `; gone ;;
*) printf '{}\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(fakeBin, "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

// After a takeover, the old herdr-core terminal is closed through the
// dismiss gates: the old agent gets its exit command and its terminal goes.
func TestCleanupClosesOldHerdrCoreTerminal(t *testing.T) {
	root, fakeBin := setupBoard(t)
	log := writeFakeHC(t, fakeBin, root, `{"app":"claude-code","state":"idle"}`)
	result := Cleanup("herdr-core:t8", launch.AgentSession{Agent: "claude", Reference: "s1"}, "herdr-core:t9", 61)
	if !result.Cleaned || result.Channel != "herdr-core" || result.Container != "t8" {
		t.Fatalf("result=%+v\n%s", result, mustReadFile(t, log))
	}
	calls := mustReadFile(t, log)
	if !strings.Contains(calls, "send --json t8 /exit") {
		t.Fatalf("calls:\n%s", calls)
	}
}

// An old terminal that has not reported its program status is kept: Kander
// cannot tell which agent runs there.
func TestCleanupKeepsUnreportedHerdrCoreTerminal(t *testing.T) {
	root, fakeBin := setupBoard(t)
	log := writeFakeHC(t, fakeBin, root, "null")
	result := Cleanup("herdr-core:t8", launch.AgentSession{Agent: "claude", Reference: "s1"}, "herdr-core:t9", 61)
	if result.Cleaned || strings.Contains(mustReadFile(t, log), "send") || strings.Contains(mustReadFile(t, log), "kill") {
		t.Fatalf("result=%+v\n%s", result, mustReadFile(t, log))
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
