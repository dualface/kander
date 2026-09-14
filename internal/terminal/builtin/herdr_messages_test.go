package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// Expected messages are the operation diagnostics at the pre-migration 2de9628.
func TestHerdrDefinitionFailureMessages(t *testing.T) {
	resetLang(t)
	backend := herdrBackendForTest()
	ctx := context.Background()
	address := terminal.Address{Container: "w1:t1", Pane: "w1:p1"}
	var value any
	invalidJSON := json.Unmarshal([]byte("not-json"), &value).Error()
	invocation := config.Text("launch.herdr_invocation_failed", "spawn failed")
	missingResult := config.Text("probe.herdr_response_is_missing_result")
	type failure struct {
		name, stdout, stderr string
		code                 int
		cause                error
	}
	failures := []failure{
		{"exec", "", "", 0, errors.New("spawn failed")},
		{"exit", "", " rejected \n", 1, nil},
		{"not-json", "not-json", "", 0, nil},
		{"not-object", "[]", "", 0, nil},
		{"missing-result", "{}", "", 0, nil},
	}
	operations := []struct {
		name string
		run  func(terminal.Conn) error
		want []string
	}{
		{"facts", func(c terminal.Conn) error { _, err := backend.PaneFacts(ctx, c, address.Pane); return err }, []string{
			invocation, config.Text("launch.pane_does_not_exist", address.Pane, "rejected"),
			config.Text("probe.herdr_pane_get_failed_response_is_not_json", invalidJSON),
			config.Text("probe.herdr_pane_get_failed_response_is_not_a_json"),
			config.Text("probe.herdr_pane_get_failed_response_is_missing_result"),
		}},
		{"create", func(c terminal.Conn) error {
			_, err := backend.CreateContainer(c, terminal.Target{}, "/project", "task")
			return err
		}, []string{
			invocation, config.Text("launch.herdr_failed", "tab create", "rejected"),
			config.Text("launch.herdr_failed_response_is_not_json", "tab create", invalidJSON),
			config.Text("launch.herdr_failed_response_is_not_a_json_object", "tab create"),
			config.Text("launch.herdr_failed_response_is_missing_result", "tab create"),
		}},
		{"lookup", func(c terminal.Conn) error {
			_, err := backend.ReverseLookup(ctx, c, terminal.Identity{Agent: "codex", Reference: "s1"})
			return err
		}, []string{
			"spawn failed", "rejected", invalidJSON, missingResult, missingResult,
		}},
		{"topology", func(c terminal.Conn) error { _, err := backend.Topology(ctx, c, address); return err }, []string{
			"spawn failed", config.Text("takeover.herdr_pane_list_failed", "rejected"),
			config.Text("takeover.herdr_pane_list_failed", invalidJSON),
			config.Text("takeover.herdr_pane_list_failed", missingResult),
			config.Text("takeover.herdr_pane_list_failed", missingResult),
		}},
	}
	for _, op := range operations {
		for index, fail := range failures {
			t.Run(op.name+"/"+fail.name, func(t *testing.T) {
				conn := terminal.Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
					return probe.Result{Stdout: fail.stdout, Stderr: fail.stderr, Code: fail.code}, fail.cause
				}}
				err := op.run(conn)
				if err == nil || probe.FailureDetail(err) != op.want[index] {
					t.Fatalf("error=%v; want=%s", err, op.want[index])
				}
			})
		}
	}
	for _, cleanup := range []bool{false, true} {
		for _, fail := range failures[:2] {
			name := "close/"
			if cleanup {
				name = "create-cleanup/"
			}
			t.Run(name+fail.name, func(t *testing.T) {
				conn := terminal.Conn{Run: func(_ context.Context, _ string, args []string) (probe.Result, error) {
					if args[1] == "create" {
						return probe.Result{Stdout: "{\"result\":{\"tab\":{\"tab_id\":\"w1:t1\"}}}"}, nil
					}
					return probe.Result{Stderr: fail.stderr, Code: fail.code}, fail.cause
				}}
				detail := "rejected"
				if fail.cause != nil {
					detail = invocation
				}
				want := config.Text("launch.failed_to_close_tab", address.Container, detail)
				var err error
				if cleanup {
					_, err = backend.CreateContainer(conn, terminal.Target{}, "/project", "task")
					want = config.Text("launch.herdr_tab_create_failed_response_is_missing_tab_or", want)
				} else {
					err = backend.CloseContainer(ctx, conn, address)
				}
				if err == nil || probe.FailureDetail(err) != want {
					t.Fatalf("error=%v; want=%s", err, want)
				}
			})
		}
	}
}
