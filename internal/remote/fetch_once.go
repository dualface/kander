package remote

import (
	"context"
	"sync"
)

// fetchOnceKey carries the fetches one command already completed.
type fetchOnceKey struct{}

// fetchMemo records the origin fetches that succeeded in one command, keyed by
// the repository and the fetched ref name or SHA.
type fetchMemo struct {
	mu   sync.Mutex
	done map[string]bool
}

// WithFetchOnce returns a context in which a fetch from origin with the same
// repository and the same ref name or SHA runs at most once after it
// succeeded. The record lives only as long as the context: it never carries
// objects of an earlier command as remote evidence, and every ls-remote that
// observes a ref still asks origin.
func WithFetchOnce(ctx context.Context) context.Context {
	return context.WithValue(ctx, fetchOnceKey{}, &fetchMemo{done: map[string]bool{}})
}

// fetchOrigin fetches one ref name or SHA from origin without tags.
func fetchOrigin(ctx context.Context, dir, target string) error {
	memo, _ := ctx.Value(fetchOnceKey{}).(*fetchMemo)
	key := dir + "\x00" + target
	if memo != nil {
		memo.mu.Lock()
		done := memo.done[key]
		memo.mu.Unlock()
		if done {
			return nil
		}
	}
	if _, err := gitCommand(ctx, dir, "fetch", "--no-tags", "origin", target); err != nil {
		return err
	}
	if memo != nil {
		memo.mu.Lock()
		memo.done[key] = true
		memo.mu.Unlock()
	}
	return nil
}
