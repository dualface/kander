package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// Preserve the assertions from the blocked review's prerequisite reproduction.
func TestHerdrDefinitionPrepareAndTopologyMessages(t *testing.T) {
	resetLang(t)
	for _, tc := range []struct {
		name, inside, workspace, message string
		binary                           bool
		lookups                          int
	}{
		{"outside-before-path", "", "", "launch.not_currently_in_herdr_the_herdr_launcher_requires_herdr", false, 0},
		{"missing-binary-before-workspace", "1", "", "launch.herdr_is_not_in_path_run_kander_welcome_to", false, 1},
		{"missing-workspace", "1", "", "launch.herdr_workspace_id_is_missing_cannot_create_a_tab", true, 1},
		{"blank-workspace", "1", " \t\n", "launch.herdr_workspace_id_is_missing_cannot_create_a_tab", true, 1},
		{"trimmed-workspace", "1", " w1 \n", "", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := envOf(map[string]string{"HERDR_ENV": tc.inside, "HERDR_WORKSPACE_ID": tc.workspace})
			backend, err := DefinitionBackend("herdr", "herdr", getenv)
			if err != nil {
				t.Fatal(err)
			}
			if got := backend.AutoDetect(getenv); got != (tc.inside == "1") {
				t.Fatalf("auto=%v; expected detection to depend only on HERDR_ENV", got)
			}
			lookups := 0
			target, err := backend.Prepare(terminal.PrepareRequest{
				Getenv: getenv, Project: "/project",
				LookPath: func(string) (string, error) {
					lookups++
					if !tc.binary {
						return "", errors.New("not found")
					}
					return "/bin/herdr", nil
				},
			})
			if lookups != tc.lookups {
				t.Errorf("PATH lookups=%d; want=%d", lookups, tc.lookups)
			}
			if tc.message != "" {
				want := config.Text(tc.message)
				if err == nil || probe.FailureDetail(err) != want {
					t.Fatalf("error=%v; want=%s", err, want)
				}
			} else if err != nil || target.Workspace != "w1" {
				t.Fatalf("target=%+v error=%v; want normalized workspace w1", target, err)
			} else {
				r := &recorder{reply: func([]string) probe.Result {
					return probe.Result{Stdout: "{\"result\":{\"tab\":{\"tab_id\":\"w1:t1\"},\"root_pane\":{\"pane_id\":\"w1:p1\"}}}"}
				}}
				if _, err := backend.CreateContainer(r.conn(), target, "/project", "task"); err != nil {
					t.Fatal(err)
				}
				assertCalls(t, r.calls, [][]string{{"tab", "create", "--workspace", "w1", "--cwd", "/project", "--label", "task", "--no-focus"}})
			}
		})
	}
	t.Run("non-object-topology-row", func(t *testing.T) {
		backend := herdrBackendForTest()
		conn := terminal.Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
			return probe.Result{Stdout: "{\"result\":{\"panes\":[42]}}"}, nil
		}}
		_, err := backend.Topology(context.Background(), conn, terminal.Address{Container: "w1:t1"})
		want := config.Text("takeover.herdr_pane_list_response_contains_an_invalid_pane")
		if err == nil || probe.FailureDetail(err) != want {
			t.Fatalf("error=%v; want=%s", err, want)
		}
	})
	t.Run("topology-row-check-order", func(t *testing.T) {
		backend := herdrBackendForTest()
		for _, tc := range []struct {
			row, message string
		}{
			{"null", "takeover.herdr_pane_list_response_contains_an_invalid_pane"},
			{"[]", "takeover.herdr_pane_list_response_contains_an_invalid_pane"},
			{"{}", "takeover.a_pane_in_the_herdr_pane_list_response_has"},
			{"{\"tab_id\":\"w1:t1\"}", "takeover.a_pane_in_the_herdr_pane_list_response_has"},
			{"{\"tab_id\":\"w1:t1\",\"pane_id\":\"w1:p1\"}", ""},
		} {
			conn := terminal.Conn{Run: func(context.Context, string, []string) (probe.Result, error) {
				return probe.Result{Stdout: "{\"result\":{\"panes\":[" + tc.row + "]}}"}, nil
			}}
			got, err := backend.Topology(context.Background(), conn, terminal.Address{Container: "w1:t1"})
			if tc.message != "" {
				if err == nil || probe.FailureDetail(err) != config.Text(tc.message) {
					t.Fatalf("row=%s error=%v; want=%s", tc.row, err, config.Text(tc.message))
				}
			} else if err != nil || len(got.Panes) != 1 || got.Panes[0] != "w1:p1" {
				t.Fatalf("row=%s topology=%+v error=%v", tc.row, got, err)
			}
		}
	})
}
