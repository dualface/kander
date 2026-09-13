package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/probe"
)

func TestExecuteDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result probe.Result
		err    error
		want   string
	}{
		{"launch", probe.Result{}, errors.New("spawn failed"), "spawn failed"},
		{"stdout", probe.Result{Code: 1, Stdout: "rejected"}, nil, "rejected"},
		{"exit", probe.Result{Code: 7}, nil, "exit 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := Conn{Program: "herdr", Run: func(context.Context, string, []string) (probe.Result, error) { return tc.result, tc.err }}
			err := RunStep(context.Background(), conn, []string{"tab"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
