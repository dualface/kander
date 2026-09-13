package terminalcheck

import (
	"context"
	"errors"
	"testing"

	"github.com/dualface/kander/internal/terminal"
)

type identityBackend struct {
	terminal.Backend
	caps    terminal.Capabilities
	matched bool
	err     error
	calls   int
}

func (b *identityBackend) Capabilities() terminal.Capabilities { return b.caps }
func (b *identityBackend) ReverseLookup(context.Context, terminal.Conn, terminal.Identity) (terminal.Address, error) {
	b.calls++
	if b.err != nil {
		return terminal.Address{}, b.err
	}
	if b.matched {
		return terminal.Address{Container: "c", Pane: "p"}, nil
	}
	return terminal.Address{}, &terminal.MatchError{Matches: 0, Cause: errors.New("no matching identity")}
}

func TestReverseLookupCapabilityCombinations(t *testing.T) {
	var check step
	for _, s := range methodSteps {
		if s.method == "ReverseLookup" {
			check = s
		}
	}
	for _, test := range []struct {
		name                          string
		metadata, agent, report, want bool
	}{
		{"no identity", false, false, false, false},
		{"report only", false, false, true, false},
		{"agent only", false, true, false, false},
		{"reported agent", false, true, true, true},
		{"metadata", true, false, false, true},
		{"metadata and report", true, false, true, true},
		{"metadata and agent", true, true, false, true},
		{"all identities", true, true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &identityBackend{caps: terminal.Capabilities{PaneMetadata: test.metadata, AgentIdentity: test.agent, SessionReport: test.report}, matched: test.want}
			c := &checker{backend: b, address: terminal.Address{Container: "c", Pane: "p"}}
			note, err := check.execute(c)
			if err != nil || (note == "") != test.want || b.calls != 1 {
				t.Fatalf("note=%q err=%v calls=%d", note, err, b.calls)
			}
			// Neither a false success nor missing supported identity may pass.
			b.matched = !test.want
			if _, err := check.execute(c); err == nil {
				t.Fatal("accepted the opposite lookup contract")
			}
			b.err = errors.New("ordinary failure")
			if _, err := check.execute(c); err == nil {
				t.Fatal("ordinary failure passed as unsupported")
			}
		})
	}
}
