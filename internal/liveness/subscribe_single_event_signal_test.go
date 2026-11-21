//go:build !windows

package liveness

import (
	"context"
	"errors"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// A termination signal reaches the subscription through the signal context of the command, and
// ends a single-event subscription as it ends a --follow one: the same events and a canceled
// context, which the command turns into exit status 0.
func TestSingleEventSignalEndsLikeFollow(t *testing.T) {
	run := func(follow bool) ([]string, error) {
		root, opts, _ := factsMember(t)
		opts.Refresh, opts.Heartbeat, opts.Follow = .25, 3600, follow
		ctx, stop := subscriptionSignalContext()
		defer stop()
		timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		writer := &clockEvents{onEvent: func(event groupEvent) error {
			if event.Event == "snapshot" {
				return syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
			}
			return nil
		}}
		err := subscribeContext(timeout, root, opts, writer, nil)
		if ctx.Err() == nil {
			t.Errorf("follow=%v: the signal did not cancel the signal context (err=%v)", follow, err)
		}
		return eventNames(writer.events), err
	}
	followed, followErr := run(true)
	single, singleErr := run(false)
	if !errors.Is(followErr, context.Canceled) || !errors.Is(singleErr, context.Canceled) {
		t.Fatalf("follow err=%v single err=%v", followErr, singleErr)
	}
	if !reflect.DeepEqual(followed, single) || len(single) == 0 || single[0] != "snapshot" {
		t.Fatalf("follow=%v single=%v", followed, single)
	}
}
