package focus

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestPaneFocusSocket(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		ok             bool
	}{
		{"ok", `{"id":"kander-focus","result":{"type":"pane_info","pane":{"pane_id":"w1:p3"}}}`, true},
		{"rejected", `{"id":"kander-focus","error":{"code":"pane_not_found"}}`, false},
		{"wrong-id", `{"id":"other","result":{"type":"pane_info","pane":{"pane_id":"w1:p3"}}}`, false},
		{"wrong-pane", `{"id":"kander-focus","result":{"type":"pane_info","pane":{"pane_id":"w1:p9"}}}`, false},
		{"invalid", "invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			requests := make(chan map[string]any, 1)
			go func() {
				defer server.Close()
				var request map[string]any
				_ = json.NewDecoder(server).Decode(&request)
				requests <- request
				_, _ = server.Write([]byte(tc.response + "\n"))
			}()
			err := sendPaneFocus(context.Background(), client, "w1:p3")
			if (err == nil) != tc.ok {
				t.Fatalf("error=%v", err)
			}
			request := <-requests
			if request["method"] != "pane.focus" || request["params"].(map[string]any)["pane_id"] != "w1:p3" {
				t.Fatalf("request=%v", request)
			}
		})
	}
}

func TestPaneFocusCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- sendPaneFocus(ctx, client, "p3") }()
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancellation succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("socket did not cancel")
	}
	if err := focusHerdrPane(context.Background(), "", "p3"); err == nil {
		t.Fatal("missing socket succeeded")
	}
}
