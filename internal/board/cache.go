package board

import (
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// CacheRoot returns the private cache root of this board:
// <root>/.kander/caches. The directory is not created here; EnsureCacheDir
// creates the directories that have to exist.
//
// The cache root is machine-local data below the board; it is never committed
// and never enters a card. Feature caches put their own versioned subtree below
// it, for example the issue snapshot cache under issues/v1.
func CacheRoot(root string) string {
	return control(root, "caches")
}

// EnsureCacheDir creates one private directory below CacheRoot and returns its
// absolute path. Every part is one path component; the leaf and any missing
// parent are created through the shared no-follow file layer with private
// permissions (0700 on POSIX, a protected DACL owned by the current user on
// Windows). Every component that already exists is tightened the same way, not
// only the leaf.
func EnsureCacheDir(root string, parts ...string) (string, error) {
	path := CacheRoot(root)
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || part != filepath.Base(part) || strings.ContainsAny(part, `/\`) {
			return "", kanbanError("board.invalid_board_path", path)
		}
		path = filepath.Join(path, part)
	}
	if err := fs.EnsurePrivateDirectory(root, path, true); err != nil {
		return "", err
	}
	return path, nil
}
