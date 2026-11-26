package fs

import "io"

func copyFileBytes(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	scratch := acquireReadScratch()
	defer releaseReadScratch(scratch)
	return io.CopyBuffer(dst, io.LimitReader(src, limit), *scratch)
}
