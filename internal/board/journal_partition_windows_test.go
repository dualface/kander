//go:build windows

package board

import (
	"os"
	"os/exec"
	"testing"
)

func TestJournalPartitionsRejectWindowsJunction(t *testing.T) {
	for _, partition := range []string{"pending", "committed"} {
		t.Run(partition, func(t *testing.T) {
			root := tempBoard(t)
			if err := ensureControl(root); err != nil {
				t.Fatal(err)
			}
			path := control(root, "operations", partition)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", path, outside).CombinedOutput(); err != nil {
				t.Fatalf("create junction: %v %s", err, output)
			}
			if _, err := Scan(root); err == nil {
				t.Fatal("scan followed junction")
			}
			if _, err := MigrateCards(root, InitOptions{}); err == nil {
				t.Fatal("init followed junction")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("junction evidence removed", err)
			}
		})
	}
}
