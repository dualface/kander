//go:build !linux && !darwin && !windows

package process

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestOtherPlatformsReportNoProcessIdentity(t *testing.T) {
	if ProcessIdentitySupported {
		t.Fatal("supported")
	}
	if _, err := ObserveProcess(context.Background(), os.Getpid()); !errors.Is(err, ErrProcessUnsupported) {
		t.Fatalf("err=%v", err)
	}
	if _, err := ObserveSettled(context.Background(), os.Getpid()); !errors.Is(err, ErrProcessUnsupported) {
		t.Fatalf("err=%v", err)
	}
	if _, err := BootIdentity(); !errors.Is(err, ErrProcessUnsupported) {
		t.Fatalf("err=%v", err)
	}
}
