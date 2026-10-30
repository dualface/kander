package launch

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func TestDispatchOptionsPreserveOtherOptionValues(t *testing.T) {
	for _, option := range []string{"--message", "--message-file", "--timeout", "--agent", "--launcher", "--pane"} {
		for _, value := range []string{"--kind=sync", "--dispatch-id", "--base=literal"} {
			t.Run(option+"/"+value, func(t *testing.T) {
				original := []string{"task", option, value}
				args := append(append([]string{}, original...), "--kind", "fix")
				rest, options, err := ParseDispatchOptions(args)
				if err != nil || !reflect.DeepEqual(rest, original) || options != (DispatchOptions{Kind: "fix"}) {
					t.Fatalf("literal argument reinterpreted: rest=%q options=%+v err=%v", rest, options, err)
				}
			})
		}
	}
}

// A notify with no dispatch options never turns into a fix round: such a round
// has no evidence, so it always ended in `fix binding required`.
func TestPrepareActionRefusesAReviewCardWithoutDispatchOptions(t *testing.T) {
	cards := map[string]string{
		"group member": "# Card\n\n- TASK_GROUP: 20261031-some-group\n",
		"bound card":   "# Card\n\n- TASK_GROUP:\n- DISPATCH_ID: dispatch-1\n",
	}
	for name, text := range cards {
		t.Run(name, func(t *testing.T) {
			s := board.Snapshot{Entry: board.Entry{TaskID: "20261031-card-task", State: "review"}, Text: text}
			_, active, err := PrepareAction(t.TempDir(), s, "修复", DispatchOptions{}, 61)
			if err == nil || active || !strings.Contains(err.Error(), "--kind") {
				t.Fatalf("active=%v err=%v", active, err)
			}
		})
	}
	s := board.Snapshot{Entry: board.Entry{TaskID: "20261031-card-task", State: "review"}, Text: "# Card\n\n- TASK_GROUP:\n"}
	if _, active, err := PrepareAction(t.TempDir(), s, "继续", DispatchOptions{}, 61); err != nil || active {
		t.Fatalf("a plain review card took the dispatch path: active=%v err=%v", active, err)
	}
}
