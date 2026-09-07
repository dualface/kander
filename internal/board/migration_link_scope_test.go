package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationStationaryHistoricalUnsupportedLinks(t *testing.T) {
	root := linkMigrationFixture(t)
	dir := filepath.Join(root, "archived", "20260701-history-task")
	legacyMigrationCard(t, root, "archived", filepath.Base(dir), true, "# History\n- TYPE: Chore\n- SIZE: large\n")
	body := "见 [构建日志](logs\\build.txt)，[old malformed](logs%ZZ.txt)；依据 [[RFC-7]] 定义\n<img srcset=\"https://example.com/a.png 1x, https://example.com/b.png 2x\">\n<img srcset=\"data:image/png;base64,AAAA 1x, https://example.com/b.png 2x\">\n"
	report := filepath.Join(dir, "report.md")
	if err := os.WriteFile(report, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(report)
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(report)
	afterInfo, _ := os.Stat(report)
	if string(after) != body || !afterInfo.ModTime().Equal(info.ModTime()) {
		t.Fatal("unrelated history changed")
	}
	assertLinkTargets(t, root)
}

func TestMigrationAffectedUnsupportedReferencesStillFail(t *testing.T) {
	for _, body := range []string{
		`[old](..\` + linkB + `.md)`,
		`[[../` + linkB + `.md]]`,
		`<img srcset="https://example.com/a.png 1x, ../` + linkB + `.md 2x">`,
	} {
		t.Run(body, func(t *testing.T) {
			root := linkMigrationFixture(t)
			from := filepath.Join(root, "done", linkD, "spec.md")
			data, err := os.ReadFile(from)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(from, append(data, []byte(body)...), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := MigrateCards(root, InitOptions{}); err == nil {
				t.Fatal("affected unsupported reference silently retained")
			}
			if _, err := os.Stat(filepath.Join(root, "done", linkB+".md")); err != nil {
				t.Fatal("preflight changed legacy card")
			}
		})
	}
}

func TestMigrationDoesNotRewriteProducerEvidence(t *testing.T) {
	root := linkMigrationFixture(t)
	body := "[original reviewed path](../../../../backlog/" + linkA + ".md)\n"
	var paths []string
	for _, kind := range []string{"reviews", "dispatches"} {
		dir := filepath.Join(root, "done", linkD, kind, "run")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "report.md")
		paths = append(paths, path)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := prepareLinkMigration(t, root)
	for _, file := range r.Files {
		if strings.Contains(file.Path, "reviews") || strings.Contains(file.Path, "dispatches") {
			t.Fatal("producer included")
		}
	}
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != body {
			t.Fatal("producer original changed")
		}
	}
	root = linkMigrationFixture(t)
	r = prepareLinkMigration(t, root)
	r.Files[0].Path = filepath.Join("done", linkD, "reviews", "original.md")
	if err := validateRecord(root, &r); err == nil {
		t.Fatal("replay accepted producer write")
	}
}
