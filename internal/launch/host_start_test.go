package launch

import (
	"os"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
)

// kander start takes the order of a worker's start out of its own environment
// before it starts anything, so no agent or terminal server inherits it.
func TestTakeStartOrderRemovesTheVariable(t *testing.T) {
	t.Setenv(process.EnvStartOrder, "so-0123456789abcdef")
	if got := takeStartOrder(); got != "so-0123456789abcdef" {
		t.Fatalf("order = %q", got)
	}
	if value, ok := os.LookupEnv(process.EnvStartOrder); ok {
		t.Fatalf("the order stayed in the environment: %q", value)
	}
	t.Setenv(process.EnvStartOrder, "not an identity")
	if got := takeStartOrder(); got != "" {
		t.Fatalf("a malformed order was taken: %q", got)
	}
	if got := takeStartOrder(); got != "" {
		t.Fatalf("no order = %q", got)
	}
}

// The start of an order passes the scanned PATH and the set config_home.env
// variables of its agent in the pane command: a terminal server that already
// runs would start the agent with its own environment. Another start passes
// only the execution identity.
func TestAgentStartEnvironmentOfAnOrder(t *testing.T) {
	if runtimeWindows() {
		t.Skip("the POSIX pane command")
	}
	t.Setenv("PATH", "/scanned/bin")
	t.Setenv("CLAUDE_CONFIG_DIR", "/accounts/two")
	t.Setenv("CODEX_HOME", "/accounts/codex")
	plain := agentStartEnvironment("exec-1", "", "claude")
	if len(plain) != 2 || plain[remote.EnvExecutionID] != "exec-1" {
		t.Fatalf("a start without an order = %v", plain)
	}
	values := agentStartEnvironment("exec-1", "so-0123456789abcdef", "claude")
	if values["PATH"] != "/scanned/bin" || values["CLAUDE_CONFIG_DIR"] != "/accounts/two" || values[remote.EnvExecutionID] != "exec-1" {
		t.Fatalf("order environment = %v", values)
	}
	if _, found := values["CODEX_HOME"]; found {
		t.Fatalf("another agent's variable reached the agent: %v", values)
	}
	inv := withInvocationEnvironment(LaunchPlan{Launcher: "tmux-session"}, process.ProcessInvocation{Argv: []string{"/bin/claude", "--prompt"}}, values)
	line, err := paneCommand(inv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "env CLAUDE_CONFIG_DIR=/accounts/two KANDER_COORDINATOR_EXECUTION_ID= KANDER_EXECUTION_ID=exec-1 PATH=/scanned/bin /bin/claude") {
		t.Fatalf("pane command = %s", line)
	}
}
