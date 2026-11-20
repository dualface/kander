package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenRegularFileIfExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	t.Run("missing file and parent", func(t *testing.T) {
		for _, missing := range []string{path, filepath.Join(root, "absent", "file")} {
			file, err := OpenRegularFileIfExists(root, missing)
			if file != nil || err != nil {
				t.Fatalf("%s: (nil, nil) expected: %v, %v", missing, file, err)
			}
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Fatalf("nothing is created: %v", entries)
		}
	})
	t.Run("directory", func(t *testing.T) {
		directory := filepath.Join(root, "directory")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		file, err := OpenRegularFileIfExists(root, directory)
		if file != nil {
			_ = file.Close()
		}
		if err == nil {
			t.Fatal("a directory is not a regular file")
		}
	})
	t.Run("regular file", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := OpenRegularFileIfExists(root, path)
		if err != nil || file == nil {
			t.Fatalf("open: %v, %v", file, err)
		}
		defer file.Close()
		buf := make([]byte, 7)
		if n, err := file.Read(buf); err != nil || string(buf[:n]) != "content" {
			t.Fatalf("read: %q, %v", buf[:n], err)
		}
		// On Windows the handle does not share write: another write open
		// fails while it lives, and a read open succeeds. POSIX shares both.
		writer, writeErr := os.OpenFile(path, os.O_WRONLY, 0)
		if writer != nil {
			_ = writer.Close()
		}
		reader, readErr := os.Open(path)
		if reader != nil {
			_ = reader.Close()
		}
		if readErr != nil {
			t.Fatalf("a read open succeeds: %v", readErr)
		}
		if runtime.GOOS == "windows" && writeErr == nil {
			t.Fatal("a write open must fail while the handle lives")
		}
		if runtime.GOOS != "windows" && writeErr != nil {
			t.Fatalf("a write open succeeds: %v", writeErr)
		}
	})
}
