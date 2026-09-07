//go:build windows

package board

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationRejectsWindowsStagingJunction(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	parent := control(root, "migrations", r.ID)
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	marker := filepath.Join(outside, "spec.md")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, filepath.Base(r.Migrations[0].To))
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", stage, outside).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v %s", err, output)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("migration followed junction")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "outside" {
		t.Fatal("junction target changed")
	}
	if _, err := os.Lstat(stage); err != nil {
		t.Fatal("junction removed")
	}
}

func TestMigrationWindowsLinkPathCase(t *testing.T) {
	root := linkMigrationFixture(t)
	path := filepath.Join(root, "backlog", linkA+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(data), "../done/", "../DONE/")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	assertLinkTargets(t, root)
}
