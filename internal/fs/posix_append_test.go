//go:build unix

package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const (
	appendCreateRounds = 2000
	appendCreateRacers = 8
)

// Eight goroutines open one new file at once in each round. Every open must succeed and
// every descriptor of a round must name the same file.
func TestOpenAppendFileConcurrentCreate(t *testing.T) {
	root := t.TempDir()
	for round := 0; round < appendCreateRounds; round++ {
		dir := filepath.Join(root, fmt.Sprint(round))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		target := filepath.Join(dir, "server.log")

		start := make(chan struct{})
		files := make([]*AppendFile, appendCreateRacers)
		errs := make([]error, appendCreateRacers)
		var wg sync.WaitGroup
		for i := range files {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				files[i], errs[i] = OpenAppendFile(root, target)
			}()
		}
		close(start)
		wg.Wait()

		var first Identity
		var haveFirst bool
		var failure error
		for i, file := range files {
			if errs[i] != nil {
				failure = errs[i]
				continue
			}
			id, err := posixIdentity(int(file.Fd()))
			if err != nil {
				failure = err
				continue
			}
			if !haveFirst {
				first, haveFirst = id, true
			} else if id != first {
				failure = fmt.Errorf("descriptors name different files: %v and %v", first, id)
			}
		}
		for _, file := range files {
			if file != nil {
				// Skip the fsync of AppendFile.Close: 16000 of them would dominate the run.
				_ = file.File.Close()
			}
		}
		if failure != nil {
			t.Fatalf("round %d: %v", round, failure)
		}
	}
}
