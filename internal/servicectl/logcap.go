package servicectl

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// LogCap is the size above which a serving process trims its own server.log,
// and the size the trim keeps.
const LogCap = 64 << 20

// LogCapInterval is how often a serving process checks the size of its own
// server.log.
const LogCapInterval = time.Hour

// LogCapper keeps <DataDir>/server.log at most Limit bytes while Log is that
// file: the background start points the server's stderr at it. A server whose
// log goes anywhere else, a terminal or a file the user chose, trims nothing,
// so the decision follows the file the process actually writes to and not the
// arguments it was started with.
type LogCapper struct {
	DataDir string
	// Log is the file the process writes its log to, normally os.Stderr.
	Log *os.File
	// Limit and Interval default to LogCap and LogCapInterval.
	Limit    int64
	Interval time.Duration
	Logger   *slog.Logger
}

// Run checks at once and then every interval until ctx ends. A trim shares
// TrimLog's process lock with an online gc, so the two never interleave.
func (c LogCapper) Run(ctx context.Context) {
	limit, interval := c.Limit, c.Interval
	if limit <= 0 {
		limit = LogCap
	}
	if interval <= 0 {
		interval = LogCapInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		c.check(limit)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// check trims the log once when it is the process log and above limit.
func (c LogCapper) check(limit int64) {
	own, err := WritesLog(c.DataDir, c.Log)
	if err != nil {
		c.Logger.Warn("server log size check", "error", err)
		return
	}
	if !own {
		return
	}
	trim, err := TrimLog(c.DataDir, limit, false)
	if err != nil {
		c.Logger.Error("server log trim failed", "path", trim.Path, "error", err)
		return
	}
	if trim.Bytes > 0 || trim.Recovered > 0 {
		c.Logger.Info("server log trimmed", "path", trim.Path, "size", trim.Size, "archived_bytes", trim.Bytes, "recovered_bytes", trim.Recovered, "archive", LogArchiveName)
	}
}

// WritesLog reports whether log is <dataDir>/server.log: the same file by
// identity (device and inode on POSIX, volume and file index on Windows). A
// missing server.log, or one that is a symbolic link, is not the log.
func WritesLog(dataDir string, log *os.File) (bool, error) {
	if log == nil {
		return false, nil
	}
	written, err := log.Stat()
	if err != nil {
		return false, err
	}
	named, err := os.Lstat(filepath.Join(dataDir, LogName))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return named.Mode().IsRegular() && os.SameFile(written, named), nil
}
