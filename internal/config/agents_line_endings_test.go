package config

import (
	"bytes"
	"io/fs"
	"testing"
)

// TestEmbeddedFilesUseLF keeps carriage returns out of the binary. A Windows
// checkout that converted line endings embedded CRLF files, and an installed
// rules extension then read as modified against the copy of an LF build.
func TestEmbeddedFilesUseLF(t *testing.T) {
	count := 0
	err := fs.WalkDir(embeddedAgentFS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(embeddedAgentFS, name)
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
