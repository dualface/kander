package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/terminal"
	"github.com/dualface/kander/internal/terminal/builtin"
)

// deliveredBackend keeps the definition identity of a real backend and
// acknowledges every delivery at once, so only the reported channel is tested.
type deliveredBackend struct {
	terminal.Backend
	marker string
}

// DefinitionName forwards the optional method the embedded interface hides.
func (b *deliveredBackend) DefinitionName() string {
	return b.Backend.(*terminal.DeclarativeBackend).DefinitionName()
}

func (b *deliveredBackend) DeliverText(context.Context, terminal.Conn, string, string) error {
	return nil
}

func (b *deliveredBackend) WaitOutput(context.Context, terminal.Conn, string, string, int) error {
	return nil
}

func (b *deliveredBackend) ReadOutput(context.Context, terminal.Conn, string) (string, error) {
	return b.marker, nil
}

// definitionBackend builds one launcher of an embedded definition, optionally
// renamed so the definition name, binary, and launcher all differ.
func definitionBackend(t *testing.T, name, launcher, rename, binary string) terminal.Backend {
	t.Helper()
	def, err := builtin.Definition(name)
	if err != nil {
		t.Fatal(err)
	}
	if rename != "" {
		def.Name, def.Binary = rename, binary
		def.Launchers = map[string]terminal.LauncherDefinition{rename + "-session": def.Launchers[launcher]}
		launcher = rename + "-session"
	}
	backend, err := terminal.NewDeclarativeBackend(def, launcher, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

// The progress line names the channel after the terminal definition, never
// after its executable or launcher. The command-level result line is covered
// by TestTerminalDefinitionLifecycle.
func TestDirectChannelNamesTheDefinition(t *testing.T) {
	resetLang(t)
	cases := []struct {
		label   string
		backend terminal.Backend
		want    string
	}{
		{"herdr", definitionBackend(t, "herdr", "herdr", "", ""), "herdr-direct"},
		{"tmux", definitionBackend(t, "tmux", "tmux", "", ""), "tmux-direct"},
		{"tmux-session", definitionBackend(t, "tmux", "tmux-session", "", ""), "tmux-direct"},
		{"luvus", definitionBackend(t, "luvus", "luvus", "", ""), "luvus-direct"},
		{"custom", definitionBackend(t, "tmux", "tmux", "dsh", "curl"), "dsh-direct"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			marker := "ack-" + tc.label
			target := DirectTarget{Backend: &deliveredBackend{Backend: tc.backend, marker: marker}, Program: "pi", PaneID: "p1", Timeout: 1}
			var confirmed bool
			out, _, err := capture(t, func() error {
				var err error
				confirmed, _, err = directNotify(target, "line", marker, target.Timeout)
				return err
			})
			if err != nil || !confirmed {
				t.Fatalf("confirmed=%v err=%v", confirmed, err)
			}
			if !strings.Contains(out, "通道="+tc.want+"\n") {
				t.Fatalf("progress line=%q want %s", out, tc.want)
			}
		})
	}
}
