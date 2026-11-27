package fs

import (
	"path/filepath"
	"runtime"
	"sync"
)

// syncWorkers bounds the parallel flushes of SyncTree where the platform has
// no file system wide flush.
var syncWorkers = min(8, runtime.GOMAXPROCS(0))

// eachParallel runs fn for every item on a bounded number of goroutines and
// returns the first error. Items not yet started when an error occurs are
// skipped.
func eachParallel(items []string, fn func(string) error) error {
	work := make(chan string)
	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
		stop  = make(chan struct{})
	)
	for range syncWorkers {
		wg.Go(func() {
			for item := range work {
				if err := fn(item); err != nil {
					once.Do(func() {
						first = err
						close(stop)
					})
				}
			}
		})
	}
feed:
	for _, item := range items {
		select {
		case work <- item:
		case <-stop:
			break feed
		}
	}
	close(work)
	wg.Wait()
	return first
}

// treePaths joins slash-separated paths relative to dir.
func treePaths(dir string, rels []string) []string {
	paths := make([]string, len(rels))
	for i, rel := range rels {
		paths[i] = filepath.Join(dir, filepath.FromSlash(rel))
	}
	return paths
}
