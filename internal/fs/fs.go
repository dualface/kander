// Package fs is Kander's cross-platform safe file boundary.
//
// On POSIX it opens component by component with openat and O_NOFOLLOW, creates private files 0600 and private directories 0700,
// takes blocking exclusive locks with flock, and replaces atomically with rename/os.replace-equivalent semantics.
// On Windows it rejects every reparse point component by component from the volume/UNC anchor and performs reads, writes,
// atomic replacement and locking through validated handles; a private object gets a protected DACL owned solely by the current user
// at creation time, never published with inherited ACLs and tightened afterwards. Any failure of a safe backend is reported explicitly,
// never silently downgraded to the ordinary path API.
package fs

import (
	"errors"
	"fmt"
	"os"
)

const (
	privateFileMode       os.FileMode = 0o600
	privateDirMode        os.FileMode = 0o700
	inheritedDirMode      os.FileMode = 0o777
	tempCleanupMaxDepth               = 64
	tempCleanupMaxEntries             = 4096
	tempCleanupMaxPasses              = 16
	tempNameAttempts                  = 128
)

// ErrUnsafe means the path escaped its boundary, contains a reparse point or symlink, or the final object type violates the contract.
var ErrUnsafe = errors.New("unsafe path")

// ErrTempCleanup means a private temporary directory could not be fully cleaned up through its pinned handle.
var ErrTempCleanup = errors.New("private temporary directory cleanup failed")

// Kind is the type of a direct member as enumerated without following links.
type Kind string

const (
	KindFile      Kind = "file"
	KindDirectory Kind = "directory"
	KindOther     Kind = "other"
)

// DirEntry is a direct member returned by ListDirectory. Callers must still open the leaf object through this package's primitives.
type DirEntry struct {
	Name string
	Kind Kind
}

// Identity is the stable identity of a pinned object. POSIX uses Dev/Ino; Windows uses Volume and FileIndex.
type Identity struct {
	Dev       uint64
	Ino       uint64
	Volume    uint32
	FileIndex uint64
}

func (id Identity) equal(other Identity) bool {
	return id.Dev == other.Dev && id.Ino == other.Ino && id.Volume == other.Volume && id.FileIndex == other.FileIndex
}

type pathError struct {
	Op   string
	Path string
	Err  error
}

func (e *pathError) Error() string {
	if e.Path == "" {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op + " " + e.Path + ": " + e.Err.Error()
}

func (e *pathError) Unwrap() error { return e.Err }

func failClosed(op, path, msg string) error {
	return &pathError{Op: op, Path: path, Err: fmt.Errorf("%w: %s", ErrUnsafe, msg)}
}

func wrap(op, path string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrUnsafe) || errors.Is(err, ErrTempCleanup) {
		if _, ok := err.(*pathError); ok {
			return err
		}
		return &pathError{Op: op, Path: path, Err: err}
	}
	return &pathError{Op: op, Path: path, Err: err}
}

func existError(op, path, msg string) error {
	return &pathError{Op: op, Path: path, Err: fmt.Errorf("%s: %w", msg, os.ErrExist)}
}

func notExistError(op, path, msg string) error {
	return &pathError{Op: op, Path: path, Err: fmt.Errorf("%s: %w", msg, os.ErrNotExist)}
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func isExist(err error) bool {
	return errors.Is(err, os.ErrExist)
}

// ExclusiveLock is a cross-process blocking exclusive lock. Unlock must be paired with it.
type ExclusiveLock struct {
	file *os.File
}

// AppendFile is an append stream on a pinned leaf handle. Close syncs before closing.
type AppendFile struct {
	*os.File
}

// Close syncs before closing, matching the fsync-on-exit behavior of onevoke.
func (f *AppendFile) Close() error {
	if f == nil || f.File == nil {
		return nil
	}
	syncErr := f.File.Sync()
	closeErr := f.File.Close()
	f.File = nil
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// TempDir is a private temporary directory with a pinned identity. Close cleans it up through the same root handle.
// POSIX unlinks a symlink member as a non-directory (removing the link only); Windows fails the close on any reparse point.
type TempDir struct {
	Path string
	impl tempDirImpl
}

// Close cleans up and removes the temporary directory.
func (d *TempDir) Close() error {
	if d == nil || d.impl == nil {
		return nil
	}
	err := d.impl.close()
	d.impl = nil
	return err
}

type tempDirImpl interface {
	close() error
}

type objectKind int

const (
	kindAny objectKind = iota
	kindFile
	kindDirectory
)
