//go:build unix

package fs

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestIsSharingViolationIsAlwaysFalseOnPOSIX(t *testing.T) {
	for _, err := range []error{nil, errors.New("busy"), unix.EBUSY, unix.EACCES, &pathError{Op: "open", Path: "x", Err: unix.ETXTBSY}} {
		if IsSharingViolation(err) {
			t.Fatalf("POSIX has no share modes, but %v was reported as a sharing violation", err)
		}
	}
}
