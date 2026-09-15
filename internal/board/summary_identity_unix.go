//go:build unix

package board

import (
	"os"
	"syscall"
)

func fileIndex(_ *os.File, info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, kanbanError("board.transaction_invalid", "file identity")
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}
