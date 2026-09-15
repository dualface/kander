package fs

import "sync"

// readScratchSize is the pooled per-read scratch length. Card bodies are
// typically a few KiB; a 1 MiB buffer per read dominated allocation in the
// TUI refresh path without changing I/O semantics.
const readScratchSize = 32 * 1024

// readPreSizeLimit caps destination pre-allocation from stat size. Larger
// files still read to EOF; they just grow the destination in chunks.
const readPreSizeLimit = 8 * 1024 * 1024

var readScratchPool = sync.Pool{
	New: func() any {
		buf := make([]byte, readScratchSize)
		return &buf
	},
}

func acquireReadScratch() *[]byte {
	return readScratchPool.Get().(*[]byte)
}

func releaseReadScratch(buf *[]byte) {
	if buf == nil {
		return
	}
	*buf = (*buf)[:cap(*buf)]
	if cap(*buf) != readScratchSize {
		return
	}
	readScratchPool.Put(buf)
}

func destForSize(size int64) []byte {
	if size <= 0 {
		return nil
	}
	n := size
	if n > readPreSizeLimit {
		n = readPreSizeLimit
	}
	if int64(int(n)) != n {
		return nil
	}
	return make([]byte, 0, int(n))
}
