package review

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingDirKeepsOnlyExistingDirectories(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, in, want string }{
		{"directory", dir, dir},
		{"missing", filepath.Join(dir, "missing"), ""},
		{"file", file, ""},
		{"empty", "", ""},
	} {
		if got := existingDir(tc.in); got != tc.want {
			t.Errorf("%s: existingDir(%q)=%q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
