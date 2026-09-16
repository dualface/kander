package launch

import (
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func TestDevinSessionIsDiscoveredAfterStart(t *testing.T) {
	cfg := config.DefaultConfig()
	session, err := newAgentSession("devin", &process.AgentProgram{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Agent != "devin" || session.Reference != "" {
		t.Fatalf("session=%+v", session)
	}
	mode := config.AgentFor(cfg, "devin").Session.Mode
	if mode != "hook:devin-session" || !config.SessionDiscoversAfterStart(mode) || !config.SessionPersistsAfterStart(mode) {
		t.Fatalf("mode=%q", mode)
	}
}

func TestParseDevinSessions(t *testing.T) {
	got, err := parseDevinSessions([]byte(`[{"id":"session-2"},{"id":"session-1"},{"id":"session-2"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"session-2", "session-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
	for _, data := range []string{`not-json`, `null`, `[{"title":"missing"}]`, `[{"id":"bad id"}]`} {
		if _, err := parseDevinSessions([]byte(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}

func TestDiscoverNewDevinSessionUsesSetDifference(t *testing.T) {
	previousList := listDevinSessionsFn
	listDevinSessionsFn = func(*process.AgentProgram, string) ([]string, error) {
		return []string{"existing", "new-session"}, nil
	}
	t.Cleanup(func() { listDevinSessionsFn = previousList })
	got, err := discoverNewDevinSession(&process.AgentProgram{}, t.TempDir(), map[string]struct{}{"existing": {}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-session" {
		t.Fatalf("session=%q", got)
	}
}
