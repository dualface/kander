package launch

import (
	"os"
	"strings"
	"testing"
)

// kander start in server mode refuses an agent this version cannot use with the same error as the
// local start: an unusable entry names its reason, an unknown name says it is not known here. The
// card stays in todo and no terminal is touched.
func TestRemoteStartRefusesUnavailableAndUnknownAgents(t *testing.T) {
	skipOnWindows(t)
	cfg, direct, task, _, _ := remoteLifecycleFixture(t, "remote-unavailable-agent")
	const reason = "session.mode \"future\" is not registered in this version"
	cfg.UnusableAgents = map[string]string{"futureagent": reason}
	log := os.Getenv("KANBAN_TMUX_LOG")
	before, _ := os.ReadFile(log)
	for _, tc := range []struct {
		agent, want string
	}{
		{"futureagent", reason},
		{"newagent", ""},
	} {
		_, err := StartRemote(tc.agent, "tmux", task.TaskID)
		if err == nil {
			t.Fatalf("%s: start succeeded", tc.agent)
		}
		if err.Error() != unavailableAgentError(cfg, tc.agent).Error() {
			t.Fatalf("%s: %v", tc.agent, err)
		}
		if !strings.Contains(err.Error(), tc.agent) || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: the error must name the agent and the reason %q: %v", tc.agent, tc.want, err)
		}
		if strings.Contains(err.Error(), "Unsupported Agent") {
			t.Fatalf("%s: the generic refusal text leaked: %v", tc.agent, err)
		}
	}
	after, _ := os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("a refused start touched a terminal")
	}
	current, err := direct.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.State != "todo" || current.Claim != nil {
		t.Fatalf("a refused start changed the card: %+v %v", current, err)
	}
}
