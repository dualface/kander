package fs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivateStreamingFileIsExclusiveAndReadIsBounded(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive-member")
	f, err := CreatePrivateRegularFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(f, "abcdef")
	if err := errors.Join(writeErr, f.Close()); err != nil {
		t.Fatal(err)
	}
	if f, err := CreatePrivateRegularFile(root, path); !errors.Is(err, os.ErrExist) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("created existing file: %v", err)
	}
	var got bytes.Buffer
	if n, err := CopyRegularFile(&got, root, path, 3); n != 3 || err != nil || got.String() != "abc" {
		t.Fatalf("bounded copy = %d %q %v", n, got.String(), err)
	}
}

func TestInheritedStreamingFileIsExclusiveAndFollowsTheUmask(t *testing.T) {
	old := unixUmask(0o022)
	defer unixUmask(old)
	root := t.TempDir()
	path := filepath.Join(root, "received-member")
	f, err := CreateInheritedRegularFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(f, "abc")
	if err := errors.Join(writeErr, f.Close()); err != nil {
		t.Fatal(err)
	}
	if f, err := CreateInheritedRegularFile(root, path); !errors.Is(err, os.ErrExist) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("created existing file: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("inherited file mode: %v %v", info, err)
	}
}
