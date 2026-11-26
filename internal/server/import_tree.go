package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"

	"github.com/dualface/kander/v2/internal/fs"
)

// treeScan is a digested directory tree: its regular files (sorted,
// root-relative), the SHA-256 of each, the digest of all of them by path, and
// their total size.
type treeScan struct {
	files  []string
	dirs   []string
	sums   map[string][sha256.Size]byte
	sizes  map[string]int64
	digest string
	bytes  int64
}

// scanTree lists every regular file below root and digests their paths and
// bytes. Symlinks, junctions, and other entries refuse the scan, as every
// board operation does. count, when set, is called after each digested file
// with the files and bytes done and the file total.
func scanTree(root string, count func(done, total int, bytes int64)) (treeScan, error) {
	return scanImportTree(context.Background(), root, count, nil)
}

func scanImportTree(ctx context.Context, root string, count func(done, total int, bytes int64), limits *uploadLimits) (treeScan, error) {
	var files, dirs []string
	var walk func(rel string) error
	walk = func(rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := fs.ListDirectory(root, filepath.Join(root, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			child := filepath.Join(rel, e.Name)
			slash := filepath.ToSlash(child)
			if limits != nil && !validUploadPath(slash) {
				return importRejected(fmt.Sprintf("invalid source board path %q", slash))
			}
			switch e.Kind {
			case fs.KindDirectory:
				dirs = append(dirs, slash)
				if limits != nil && len(dirs) > limits.dirs {
					return uploadTooLarge()
				}
				if err := walk(child); err != nil {
					return err
				}
			case fs.KindFile:
				files = append(files, slash)
				if limits != nil && len(files) > limits.files {
					return uploadTooLarge()
				}
			default:
				return errors.New("board entry " + child + " is not a regular file or directory")
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return treeScan{}, err
	}
	slices.Sort(files)
	slices.Sort(dirs)
	scan := treeScan{files: files, dirs: dirs, sums: make(map[string][sha256.Size]byte, len(files)), sizes: make(map[string]int64, len(files))}
	h := sha256.New()
	for i, rel := range files {
		if err := ctx.Err(); err != nil {
			return treeScan{}, err
		}
		fileLimit := int64(math.MaxInt64)
		if limits != nil {
			fileLimit = min(limits.fileBytes, limits.bytes-scan.bytes) + 1
		}
		hash := sha256.New()
		size, err := fs.CopyRegularFile(hash, root, filepath.Join(root, filepath.FromSlash(rel)), fileLimit)
		if err != nil {
			return treeScan{}, err
		}
		if limits != nil && size == fileLimit {
			return treeScan{}, uploadTooLarge()
		}
		sum := [sha256.Size]byte(hash.Sum(nil))
		scan.sums[rel] = sum
		scan.sizes[rel] = size
		scan.bytes += scan.sizes[rel]
		hashImportEntry(h, rel, sum)
		if count != nil {
			count(i+1, len(files), scan.bytes)
		}
	}
	scan.digest = hex.EncodeToString(h.Sum(nil))
	return scan, nil
}

// snapshotTree lists every regular file below root and digests their paths and
// bytes.
func snapshotTree(root string) ([]string, string, error) {
	scan, err := scanTree(root, nil)
	return scan.files, scan.digest, err
}

// hashImportEntry is the existing local snapshot digest framing.
func hashImportEntry(w io.Writer, path string, sum [sha256.Size]byte) {
	fmt.Fprintf(w, "%s\x00%x\n", path, sum)
}
