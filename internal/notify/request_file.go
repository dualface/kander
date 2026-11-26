package notify

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/dualface/kander/v2/internal/fs"
)

func writeRequestFile(body string) (*fs.TempDir, string, error) {
	root, err := notificationTempRoot()
	if err != nil {
		return nil, "", err
	}
	return writeRequestFileIn(root, body)
}

func writeRequestFileIn(root, body string) (*fs.TempDir, string, error) {
	for range 128 {
		dir, err := fs.CreatePrivateTempDir(root, "kander-request-")
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		path := filepath.Join(dir.Path, "request.md")
		if err := fs.WriteTextAtomic(dir.Path, path, body, false); err != nil {
			return nil, "", errors.Join(err, dir.Close())
		}
		return dir, path, nil
	}
	return nil, "", errors.New("could not allocate a private request directory")
}
