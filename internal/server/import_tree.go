package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/dualface/kander/v2/internal/fs"
)

// treeScan is a digested directory tree: its regular files (sorted,
// root-relative), the SHA-256 of each, the digest of all of them by path, and
// their total size.
type treeScan struct {
	files  []string
	sums   map[string][sha256.Size]byte
	digest string
	bytes  int64
}

// scanTree lists every regular file below root and digests their paths and
// bytes. Symlinks, junctions, and other entries refuse the scan, as every
// board operation does. count, when set, is called after each digested file
// with the files and bytes done and the file total.
func scanTree(root string, count func(done, total int, bytes int64)) (treeScan, error) {
	var files []string
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := fs.ListDirectory(root, filepath.Join(root, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			child := filepath.Join(rel, e.Name)
			switch e.Kind {
			case fs.KindDirectory:
				if err := walk(child); err != nil {
					return err
				}
			case fs.KindFile:
				files = append(files, filepath.ToSlash(child))
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
	scan := treeScan{files: files, sums: make(map[string][sha256.Size]byte, len(files))}
	h := sha256.New()
	for i, rel := range files {
		data, err := fs.ReadRegularFile(root, filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return treeScan{}, err
		}
		sum := sha256.Sum256(data)
		scan.sums[rel] = sum
		scan.bytes += int64(len(data))
		fmt.Fprintf(h, "%s\x00%x\n", rel, sum)
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
