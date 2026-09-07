package liveness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// A new test process models subscriber restart without sharing memory or a cursor.
func TestSubscribeRestartProcess(t *testing.T) {
	if os.Getenv("KANDER_FACTS_CHILD") == "1" {
		stop := make(chan struct{})
		writer := &clockEvents{onEvent: func(event groupEvent) error {
			data, err := json.Marshal(event)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			close(stop)
			return nil
		}}
		opts := subscribeOptions{Group: os.Getenv("KANDER_FACTS_GROUP"), Members: []string{os.Getenv("KANDER_FACTS_MEMBER")}, Refresh: .001, Heartbeat: 1}
		if err := Subscribe(os.Getenv("KANDER_FACTS_ROOT"), opts, writer, stop); err != nil {
			t.Fatal(err)
		}
		return
	}
	root, opts, id := factsMember(t)
	readChild := func() groupEvent {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSubscribeRestartProcess$")
		cmd.Env = append(os.Environ(), "KANDER_FACTS_CHILD=1", "KANDER_FACTS_ROOT="+root, "KANDER_FACTS_GROUP="+opts.Group, "KANDER_FACTS_MEMBER="+id)
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("restart child: %v %s", err, raw)
		}
		var event groupEvent
		if err = json.Unmarshal([]byte(firstJSONLine(string(raw))), &event); err != nil {
			t.Fatalf("snapshot: %v %s", err, raw)
		}
		return event
	}
	dispatch := prepareSubscriptionDispatch(t, root, id, "sync", time.Minute)
	before := readChild()
	completeSubscriptionDispatch(t, root, id, dispatch, "review")
	after := readChild()
	assertSubscriptionReceipt(t, after, id, dispatch, "review")
	if before.Seq != 1 || after.Seq != 1 || before.SubscriptionID == after.SubscriptionID || after.TaskRevisions[id] != before.TaskRevisions[id]+2 || before.Tasks[id] != after.Tasks[id] {
		t.Fatalf("restart lost revision or reused cursor: before=%+v after=%+v", before, after)
	}
}
