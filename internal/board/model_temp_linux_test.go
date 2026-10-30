package board

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// modelMinimumShm keeps the search on disk where /dev/shm is a small mount, as
// in a default container. Full mode peaks near 310 MiB.
const modelMinimumShm = 1 << 30

// modelFastTemp moves the search to memory where that is possible: board writes
// sync every file, and on disk the sync dominates the run time.
func modelFastTemp(t *testing.T) {
	t.Helper()
	var stat unix.Statfs_t
	if unix.Statfs("/dev/shm", &stat) != nil || uint64(stat.Bavail)*uint64(stat.Bsize) < modelMinimumShm {
		return
	}
	dir, err := os.MkdirTemp("/dev/shm", "kander-model-")
	if err != nil {
		return
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMPDIR", dir)
}
