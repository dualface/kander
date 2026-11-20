package remote

import (
	"fmt"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// On Windows an atomic replacement keeps a temporary handle with DELETE access
// open on the target for a moment after the rename, and readers deliberately
// do not share DELETE. A lock-free read of an execution record or pointer that
// lands in that window gets a sharing violation, so the readers below wait for
// the writer to finish. Nothing here changes how either side opens the file.
//
// sharedReadWaits are the waits between attempts; their sum stays below one second.
var sharedReadWaits = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 160 * time.Millisecond, 320 * time.Millisecond}

var (
	sharedReadBusy  = fs.IsSharingViolation // tests replace it
	sharedReadSleep = time.Sleep            // tests replace it
)

// retrySharedRead runs read once and repeats it only while it fails with a
// sharing violation, following sharedReadWaits. Every attempt must reopen the
// file from the home root, so a retry cannot be redirected by a swapped path
// component. Never call it while holding a credential lock: the writers take
// those locks and the sleep would only delay them.
func retrySharedRead[T any](read func() (T, error)) (T, error) {
	value, err := read()
	for _, wait := range sharedReadWaits {
		if err == nil || !sharedReadBusy(err) {
			return value, err
		}
		sharedReadSleep(wait)
		value, err = read()
	}
	if err != nil && sharedReadBusy(err) {
		return value, fmt.Errorf("%s: %w", config.Text("remote.credential_busy"), err)
	}
	return value, err
}

func readSharedFile(home, path string) ([]byte, error) {
	return retrySharedRead(func() ([]byte, error) { return fs.ReadRegularFile(home, path) })
}

func readSharedFileIfExists(home, path string) (data []byte, found bool, err error) {
	type result struct {
		data  []byte
		found bool
	}
	r, err := retrySharedRead(func() (result, error) {
		data, found, err := fs.ReadRegularFileIfExists(home, path)
		return result{data, found}, err
	})
	return r.data, r.found, err
}

func readSharedCredentialHead(home, dir string) (head credentialHead, found bool, err error) {
	type result struct {
		head  credentialHead
		found bool
	}
	r, err := retrySharedRead(func() (result, error) {
		head, found, err := readCredentialHead(home, dir)
		return result{head, found}, err
	})
	return r.head, r.found, err
}
