package launch

import (
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestDurableProcessLivenessRequiresReceiptAtDeadline(t *testing.T) {
	for _, launcher := range []string{"foreground", "console"} {
		t.Run(launcher, func(t *testing.T) {
			root, task, d := resumeDispatchFixture(t)
			d, err := board.BeginDispatchAttempt(root, task, d.Input.ID, d.Revision)
			if err != nil {
				t.Fatal(err)
			}
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			freezeClock(t)
			outcome := LaunchOutcome{Poll: func() *int { return nil }}
			err = validateResumedDispatch(root, s.Entry, s.Text, LaunchPlan{Launcher: launcher}, outcome, AgentSession{}, time.Minute.Seconds())
			if err == nil {
				t.Fatal("live executor without receipt reported success at deadline")
			}
			current, err := board.ReadDispatch(root, task, d.Input.ID)
			if err != nil || current.State != board.DispatchUnknown || current.Accepted != nil || !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
				t.Fatalf("pending validation altered durable intent: %+v %v", current, err)
			}
			after, err := board.ReadSnapshot(root, task)
			if err != nil || after.Revision != s.Revision || after.Text != s.Text {
				t.Fatalf("pending validation rewrote executor state: %v", err)
			}
		})
	}
}
