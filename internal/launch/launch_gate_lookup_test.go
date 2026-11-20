package launch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// fakeReceipts is the read-only view of the receipts that refuseBeforeSession reads.
type fakeReceipts struct {
	found  bool
	phase  string
	err    error
	peeked int
}

func (f *fakeReceipts) PeekAction(string, any) (remote.LocalAction, bool, error) {
	f.peeked++
	return remote.LocalAction{Phase: f.phase}, f.found, f.err
}

// refuseBeforeSessionAs runs refuseBeforeSession inside a command, because the --json state it
// consults belongs to the command that runs. It returns the answer of the check and how many
// times the check looked the project up.
func refuseBeforeSessionAs(t *testing.T, withJSON bool, launcher string, lookup func(context.Context, *config.Config) (receiptReader, bool, error)) (answer error, lookups int) {
	t.Helper()
	previous := lookupHost
	t.Cleanup(func() { lookupHost = previous })
	lookupHost = func(ctx context.Context, cfg *config.Config) (receiptReader, bool, error) {
		lookups++
		return lookup(ctx, cfg)
	}
	probe := &cobra.Command{Use: "probe", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		answer = refuseBeforeSession(config.DefaultConfig(), launcher, "task-launch", true, "logical")
		return nil
	}}
	args := []string{}
	if withJSON {
		args = append(args, "--json")
	}
	commandtest.Capture(func() int { return commandtest.Run(probe, args...) })
	return answer, lookups
}

func isInteractiveRefusal(err error) bool {
	var failure *command.Failure
	return errors.As(err, &failure) && failure.Code == command.CodeInteractive
}

// The check before the session has five ways to end. Only a call that cannot be told apart from a
// start reaches the read-only lookup; a lookup or a receipt that cannot be read ends the call with
// its own error and never hands the call on to Open, which would register the project.
func TestRefuseBeforeSessionBranches(t *testing.T) {
	failure := errors.New("lookup failed: the service did not answer")
	view := func(receipts *fakeReceipts, nothing bool, err error) func(context.Context, *config.Config) (receiptReader, bool, error) {
		return func(context.Context, *config.Config) (receiptReader, bool, error) {
			if err != nil || nothing {
				return nil, nothing, err
			}
			return receipts, false, nil
		}
	}

	t.Run("without --json nothing is read", func(t *testing.T) {
		err, lookups := refuseBeforeSessionAs(t, false, "foreground", view(nil, false, failure))
		if err != nil || lookups != 0 {
			t.Fatalf("err=%v lookups=%d", err, lookups)
		}
	})
	t.Run("a launcher with a container reads nothing", func(t *testing.T) {
		err, lookups := refuseBeforeSessionAs(t, true, "tmux", view(nil, false, failure))
		if err != nil || lookups != 0 {
			t.Fatalf("err=%v lookups=%d", err, lookups)
		}
	})
	t.Run("a lookup that fails is the result", func(t *testing.T) {
		err, lookups := refuseBeforeSessionAs(t, true, "foreground", view(nil, false, failure))
		if err != failure || lookups != 1 {
			t.Fatalf("err=%v lookups=%d", err, lookups)
		}
	})
	t.Run("no receipt can exist, so the call is refused", func(t *testing.T) {
		err, _ := refuseBeforeSessionAs(t, true, "foreground", view(nil, true, nil))
		if !isInteractiveRefusal(err) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("a receipt that would be replayed lets the call through", func(t *testing.T) {
		receipts := &fakeReceipts{found: true, phase: "complete"}
		err, _ := refuseBeforeSessionAs(t, true, "foreground", view(receipts, false, nil))
		if err != nil || receipts.peeked != 1 {
			t.Fatalf("err=%v peeked=%d", err, receipts.peeked)
		}
	})
	t.Run("a receipt that cannot be read is the result", func(t *testing.T) {
		receipts := &fakeReceipts{err: failure}
		err, _ := refuseBeforeSessionAs(t, true, "foreground", view(receipts, false, nil))
		if err != failure {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("a known project without a replayable receipt is refused", func(t *testing.T) {
		for name, receipts := range map[string]*fakeReceipts{
			"no receipt":        {},
			"a prepared launch": {found: true, phase: "prepared"},
		} {
			err, _ := refuseBeforeSessionAs(t, true, "foreground", view(receipts, false, nil))
			if !isInteractiveRefusal(err) {
				t.Fatalf("%s: err=%v", name, err)
			}
		}
	})
}

// A caller that opened the session before it launched marks its input, and the mark is not part
// of the receipt key: the logical input of the receipt and the intent of a command receipt are
// the same bytes with and without it, so no stored receipt changes its key.
func TestSessionCheckedMarkStaysOutOfTheReceiptKey(t *testing.T) {
	input := remoteLaunchInput{Agent: "claude", Launcher: "foreground", TaskID: "task", Message: "go", Decision: "d", Takeover: true, Native: true}
	marked := input
	marked.SessionChecked = true
	plain, err := json.Marshal(launchReceiptInput{"task", input})
	if err != nil {
		t.Fatal(err)
	}
	withMark, err := json.Marshal(launchReceiptInput{"task", marked})
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(withMark) {
		t.Fatalf("the receipt key changed:\n%s\n%s", plain, withMark)
	}
	intent, err := json.Marshal(nativeResumeIntent{Input: marked})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(intent)), "sessionchecked") {
		t.Fatalf("the intent of a command receipt holds the mark: %s", intent)
	}
}
