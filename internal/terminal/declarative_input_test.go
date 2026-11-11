package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
)

func TestDeclarativeLiteralInput(t *testing.T) {
	backend := declarativeOp(t, OpReadOutput, `{
		"steps":[{"argv":["connect"],"stdin":"{\"literal\":\"{pane} $()\"}\n","store":"reply"}],
		"result":{"text":"{step.reply.text}"}}`, nil)
	for _, outcome := range []string{"ok", "overflow", "canceled"} {
		calls := 0
		conn := Conn{Program: "fake", Run: func(context.Context, string, []string) (probe.Result, error) {
			t.Fatal("stdin must not be discarded by the argv runner")
			return probe.Result{}, nil
		}, RunInput: func(_ context.Context, _ string, args []string, input string) (probe.Result, error) {
			calls++
			if len(args) != 1 || args[0] != "connect" || input != "{\"literal\":\"{pane} $()\"}\n" {
				t.Fatalf("args=%q input=%q", args, input)
			}
			if outcome == "canceled" {
				return probe.Result{}, context.Canceled
			}
			return probe.Result{Stdout: "reply", Overflow: outcome == "overflow"}, nil
		}}
		got, err := backend.ReadOutput(t.Context(), conn, "p1")
		if calls != 1 || (outcome == "ok" && (err != nil || got != "reply")) || (outcome == "overflow" && err == nil) || (outcome == "canceled" && !errors.Is(err, context.Canceled)) {
			t.Fatalf("outcome=%s calls=%d output=%q err=%v", outcome, calls, got, err)
		}
	}
}

func TestValidateLiteralInput(t *testing.T) {
	for _, input := range []string{strings.Repeat("x", (64<<10)+1), "a\x00b"} {
		data := mutateFixture(t, func(root map[string]any) {
			op := object(object(root, "ops"), OpReadOutput)
			op["steps"] = []any{map[string]any{"argv": []any{"read"}, "stdin": input}}
			delete(op, "result")
		})
		if _, err := DecodeDefinition("faketerm.json", data); err == nil || !strings.Contains(err.Error(), "stdin") {
			t.Fatalf("err=%v", err)
		}
	}
}
