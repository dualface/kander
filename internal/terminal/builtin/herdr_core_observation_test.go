package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func TestHerdrCoreListProtocol(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil).(terminal.AgentLister)
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":2,"result":{"terminals":[]}}`,
		`{"jsonrpc":"1.0","id":1,"result":{"terminals":[]}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32000},"result":{"terminals":[]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"terminals":null}}`,
	} {
		conn := terminal.Conn{Program: "hc", RunInput: func(_ context.Context, _ string, args []string, input string) (probe.Result, error) {
			var request struct {
				JSONRPC string         `json:"jsonrpc"`
				ID      int            `json:"id"`
				Method  string         `json:"method"`
				Params  map[string]any `json:"params"`
			}
			if err := json.Unmarshal([]byte(input), &request); err != nil || request.JSONRPC != "2.0" || request.ID != 1 || request.Method != "terminal.list" || len(request.Params) != 0 || !slices.Equal(args, []string{"connect", "--json"}) {
				t.Fatalf("request=%q args=%q err=%v", input, args, err)
			}
			return probe.Result{Stdout: body}, nil
		}}
		if panes, err := backend.AgentList(t.Context(), conn); err == nil {
			t.Fatalf("invalid RPC response accepted: %s; panes=%+v", body, panes)
		}
	}
}

func TestHerdrCoreAgentList(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil).(terminal.AgentLister)
	for _, tc := range []struct {
		name, rows string
		ok         bool
	}{
		{"live-and-remote", `[{"address":"remote/t8","state":"live"},{"address":"t8","state":"live","status":{"app":"claude-code","state":"error"},"labels":{"kander-session":"s1"}},{"address":"t9","state":"finished"}]`, true},
		{"unreported", `[{"address":"t8","state":"live","status":null}]`, true},
		{"same-name-labels", `[{"address":"t8","state":"live","status":{"app":"claude-code","state":"idle"},"labels":{"status":"ready","app":"ignored","state":"lost","labels":"ignored","address":"remote/t8","kander-session":"s1"}}]`, true},
		{"remote-with-local-label", `[{"address":"remote/t8","state":"live","labels":{"address":"t8","kander-session":"s1"}},{"address":"t8","state":"live"}]`, true},
		{"duplicate", `[{"address":"t8","state":"live"},{"address":"t8","state":"live"}]`, false},
		{"missing-address", `[{"state":"live"}]`, false},
		{"bad-address", `[{"address":"t8:bad","state":"live"}]`, false},
		{"bad-status", `[{"address":"t8","state":"live","status":42}]`, false},
		{"bad-app", `[{"address":"t8","state":"live","status":{"app":42}}]`, false},
		{"bad-status-state", `[{"address":"t8","state":"live","status":{"state":42}}]`, false},
		{"invalid-masked-by-label", `[{"address":"bad","state":"bad","labels":{"address":"t8","state":"live"}}]`, false},
		{"bad-labels", `[{"address":"t8","state":"live","labels":[]}]`, false},
		{"bad-session", `[{"address":"t8","state":"live","labels":{"kander-session":42}}]`, false},
		{"missing-state", `[{"address":"t8"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result {
				return probe.Result{Stdout: `{"jsonrpc":"2.0","id":1,"result":{"terminals":` + tc.rows + `}}`}
			}}
			panes, err := backend.AgentList(t.Context(), r.conn())
			if (err == nil) != tc.ok {
				t.Fatalf("panes=%+v err=%v", panes, err)
			}
			assertCalls(t, r.calls, [][]string{{"connect", "--json"}})
			if !tc.ok {
				return
			}
			if len(panes) != 1 || panes[0].Pane != "t8" {
				t.Fatalf("panes=%+v", panes)
			}
			facts := panes[0].Facts()
			if !facts.ProgramStatus {
				t.Fatalf("lost OSC identity: %+v", facts)
			}
			if tc.name == "live-and-remote" && (facts.AgentApp != "claude-code" || facts.AgentStatus != "blocked" || facts.AgentStatusReason != "error" || facts.SessionMarker != "s1" || facts.AgentSession != "s1") {
				t.Fatalf("lost OSC facts: %+v", facts)
			}
			if tc.name == "unreported" && facts.MatchAgent("claude") != terminal.AgentUndecided {
				t.Fatalf("unreported agent decided: %+v", facts)
			}
		})
	}
}

func TestHerdrCoreLookupFiltersRemoteAndStaleLabels(t *testing.T) {
	for _, extraLabels := range []string{"", `,"status":"ready","app":"ignored","state":"lost","labels":"ignored"`} {
		t.Run(extraLabels, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result {
				return probe.Result{Stdout: `{"jsonrpc":"2.0","id":1,"result":{"terminals":[
			{"address":"remote/t8","state":"live","labels":{"address":"t8","kander-session":"s1"}},
			{"address":"t1","state":"finished","labels":{"kander-session":"s1"}},
			{"address":"t2","state":"live","labels":{"kander-session":"other"}},
			{"address":"t8","state":"live","labels":{"kander-session":"s1"` + extraLabels + `}}]}}`}
			}}
			got, err := herdrCoreBackendForTest(t, nil).ReverseLookup(t.Context(), r.conn(), terminal.Identity{Agent: "claude", Reference: "s1"})
			if err != nil || got.Pane != "t8" || got.Container != "t8" {
				t.Fatalf("lookup=%+v err=%v", got, err)
			}
		})
	}
}

func TestHerdrCoreProcessFacts(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil).(terminal.ProcessInspector)
	for _, tc := range []struct {
		name, body string
		want       []terminal.ForegroundProcess
		ok         bool
	}{
		{"live", `{"address":"t8","state":"live","foreground":{"pid":42,"name":"node","argv":["node","agent.js"],"cwd":"/work"}}`, []terminal.ForegroundProcess{{PID: 42, Name: "node"}}, true},
		{"unavailable", `{"address":"t8","state":"live","foreground":null}`, nil, true},
		{"old-core", `{"address":"t8","state":"live"}`, nil, true},
		{"wrong-pane", `{"address":"t9","state":"live","foreground":{"pid":42,"name":"node"}}`, nil, false},
		{"finished", `{"address":"t8","state":"finished","foreground":{"pid":42,"name":"node"}}`, nil, false},
		{"invalid-pid", `{"address":"t8","state":"live","foreground":{"pid":0,"name":"node"}}`, nil, false},
		{"missing-name", `{"address":"t8","state":"live","foreground":{"pid":42}}`, nil, false},
		{"invalid-object", `{"address":"t8","state":"live","foreground":[]}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{reply: func([]string) probe.Result { return probe.Result{Stdout: tc.body} }}
			got, err := backend.ProcessFacts(t.Context(), r.conn(), "t8")
			if (err == nil) != tc.ok || (tc.ok && !slices.Equal(got, tc.want)) {
				t.Fatalf("processes=%+v err=%v", got, err)
			}
		})
	}
}

// hc can hold a draft for 60 seconds. A shorter caller deadline still wins,
// and a cancelled send is never retried or converted to success.
func TestHerdrCoreDeliveryBudget(t *testing.T) {
	backend := herdrCoreBackendForTest(t, nil)
	for _, short := range []bool{false, true} {
		ctx := t.Context()
		if short {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Millisecond)
			defer cancel()
		}
		calls := 0
		conn := terminal.Conn{Program: "hc", Run: func(ctx context.Context, _ string, _ []string) (probe.Result, error) {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || (!short && time.Until(deadline) < 60*time.Second) {
				t.Fatalf("send deadline does not cover draft protection: %v", deadline)
			}
			if short {
				<-ctx.Done()
				return probe.Result{}, ctx.Err()
			}
			// The text was delivered, but no working transition was observed.
			return probe.Result{Stdout: `{"started":false,"status":"idle","turn":1}`}, nil
		}}
		err := backend.DeliverText(ctx, conn, "t8", "task")
		if (short && !errors.Is(err, context.DeadlineExceeded)) || (!short && err != nil) || calls != 1 {
			t.Fatalf("short=%v calls=%d err=%v", short, calls, err)
		}
	}
}
