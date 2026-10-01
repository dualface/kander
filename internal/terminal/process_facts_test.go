package terminal

import (
	"errors"
	"testing"

	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func TestDeclarativeProcessFacts(t *testing.T) {
	backend := declarativeOp(t, OpProcessFacts, `{
		"steps":[{"argv":["process","{pane}"],"store":"job",
		"output":{"source":"stdout","parse":"json_field:result"},
		"fields":{"pane":"json_field:pane_id","processes":"json_field:foreground_processes"}}],
		"result":{"pane":"{step.job.pane}","processes":"{step.job.processes}"}}`, nil)
	for _, tc := range []struct {
		name, output string
		ok           bool
	}{
		{"valid", `{"result":{"pane_id":"p1","foreground_processes":[{"pid":42,"name":"codex","argv":[]}]}}`, true},
		{"empty", `{"result":{"pane_id":"p1","foreground_processes":[]}}`, true},
		{"wrong-pane", `{"result":{"pane_id":"p2","foreground_processes":[{"pid":42,"name":"codex"}]}}`, false},
		{"duplicate-pid", `{"result":{"pane_id":"p1","foreground_processes":[{"pid":42,"name":"codex"},{"pid":42,"name":"codex"}]}}`, false},
		{"invalid-pid", `{"result":{"pane_id":"p1","foreground_processes":[{"pid":0,"name":"codex"}]}}`, false},
		{"missing-processes", `{"result":{"pane_id":"p1"}}`, false},
		{"invalid-array", `{"result":{"pane_id":"p1","foreground_processes":"invalid"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := terminaltest.New(t, reply(tc.output, "process", "p1"))
			facts, err := backend.ProcessFacts(t.Context(), fakeConn(fake), "p1")
			if (err == nil) != tc.ok {
				t.Fatalf("facts=%+v err=%v", facts, err)
			}
			if tc.name == "valid" && (len(facts) != 1 || facts[0].PID != 42 || facts[0].Name != "codex") {
				t.Fatalf("facts=%+v", facts)
			}
		})
	}
	if _, err := fixtureBackend(t, noEnv).ProcessFacts(t.Context(), Conn{}, "p1"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("old definition: %v", err)
	}
}
