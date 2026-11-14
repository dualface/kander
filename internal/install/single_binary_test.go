package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallIgnoresObsoleteServerSibling(t *testing.T) {
	setupInstallHome(t)
	source := stubBinary(t)
	obsolete := filepath.Join(filepath.Dir(source), componentBinaryName("kander-server"))
	// A directory at the obsolete name used to fail companion preflight.
	if err := os.Mkdir(obsolete, 0700); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{CopyBinary: true, Language: "en", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(result.Paths.BinDir, componentBinaryName("kander-server"))); !os.IsNotExist(err) {
		t.Fatalf("installer copied obsolete component: %v", err)
	}
	if _, err := os.Stat(result.DestBinary); err != nil {
		t.Fatal(err)
	}
}
