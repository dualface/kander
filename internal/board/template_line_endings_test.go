package board

import (
	"bytes"
	"io/fs"
	"testing"
)

// TestEmbeddedFilesUseLF keeps carriage returns out of the binary. A Windows
// checkout that converted line endings embedded CRLF files, and a new card
// got mixed line endings once a metadata write replaced a CRLF line with LF.
func TestEmbeddedFilesUseLF(t *testing.T) {
	count := 0
	err := fs.WalkDir(templateFS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(templateFS, name)
		if err != nil {
			return err
		}
		count++
		if bytes.IndexByte(data, '\r') >= 0 {
			t.Errorf("%s contains a carriage return", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no embedded file found")
	}
}
