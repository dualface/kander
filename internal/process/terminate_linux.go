package process

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
)

// TerminateObserved pins the PID before rechecking its kernel start token.
// A PID reused after the check cannot receive this signal through the pidfd.
func TerminateObserved(ctx context.Context, facts ProcessFacts, boot string) error {
	currentBoot, err := BootIdentity()
	if err != nil {
		return err
	}
	if boot == "" || facts.Start == "" || !SameBoot(boot, currentBoot) {
		return ErrProcessReplaced
	}
	fd, err := unix.PidfdOpen(facts.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	current, err := ObserveProcess(ctx, facts.PID)
	if errors.Is(err, ErrProcessGone) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Start != facts.Start {
		return ErrProcessReplaced
	}
	err = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}
