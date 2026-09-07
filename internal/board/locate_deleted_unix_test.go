//go:build unix

package board

import (
	"os"
	"os/exec"
	"testing"
)

func TestBoardRootOverrideAfterCWDDeletion(t *testing.T) {
	if os.Getenv("KANDER_DELETED_CWD_CHILD") == "1" {
		dir := os.Getenv("KANDER_DELETED_CWD_DIR")
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Getwd(); err == nil {
			t.Fatal("cwd still available")
		}
		got, err := BoardRoot()
		if err != nil || got != os.Getenv(EnvBoardDir) {
			t.Fatalf("%q %v", got, err)
		}
		return
	}
	root := tempBoard(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBoardRootOverrideAfterCWDDeletion$")
	cmd.Env = append(os.Environ(), "KANDER_DELETED_CWD_CHILD=1", "KANDER_DELETED_CWD_DIR="+t.TempDir(), EnvBoardDir+"="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
