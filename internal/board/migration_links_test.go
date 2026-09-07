package board

import (
	"bufio"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func TestRelocateMarkdownDestinationsOnly(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "backlog", "20260907-a-task.md")
	to := filepath.Join(root, "backlog", "20260907-a-task", "spec.md")
	mapping := map[string]string{from: to, filepath.Join(root, "done", "20260907-b-task.md"): filepath.Join(root, "done", "20260907-b-task", "spec.md")}
	for key, value := range mapping {
		delete(mapping, key)
		mapping[migrationPathKey(key)] = value
	}
	cases := []struct{ name, before, after string }{
		{"inline", `[文字](../done/20260907-b-task.md?q=1&amp;x=2#标题 "title")`, `[文字](../../done/20260907-b-task/spec.md?q=1&amp;x=2#标题 "title")`},
		{"image", `![图](<图 片.png> '图题')`, `![图](<../%E5%9B%BE%20%E7%89%87.png> '图题')`},
		{"escaped", `[x](a\(b\).txt)`, `[x](../a%28b%29.txt)`},
		{"encoded", `[x](a%23b.txt?raw=%23#f)`, `[x](../a%23b.txt?raw=%23#f)`},
		{"entity", `[x](a&amp;b.txt&#35;f)`, `[x](../a%26b.txt#f)`},
		{"reference", "[label][ref]\r\n\r\n[ref]: ../done/20260907-b-task.md \"title\"\r\n", "[label][ref]\r\n\r\n[ref]: ../../done/20260907-b-task/spec.md \"title\"\r\n"},
		{"unused definitions", "[unused]: foo.txt\n[unused]: bar.txt\n", "[unused]: ../foo.txt\n[unused]: ../bar.txt\n"},
		{"multiline", "[label](\n  target.md\n  \"title\"\n)", "[label](\n  ../target.md\n  \"title\"\n)"},
		{"nested", "> - [x](target.md)", "> - [x](../target.md)"},
		{"skip", "`[x](a.md)`\n\n```md\n[x](b.md)\n```\n\n    [x](c.md)\n\n[web](https://example.com/a?q#f) [mail](mailto:x@y.com) [root](/a) [anchor](#a) [query](?x=1)\n", "`[x](a.md)`\n\n```md\n[x](b.md)\n```\n\n    [x](c.md)\n\n[web](https://example.com/a?q#f) [mail](mailto:x@y.com) [root](/a) [anchor](#a) [query](?x=1)\n"},
		{"same spelling in code", "`[x](a.md)` [x](a.md)", "`[x](a.md)` [x](../a.md)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := relocateMarkdown(c.before, from, to, mapping)
			if err != nil || got != c.after {
				t.Fatalf("got %q err %v want %q", got, err, c.after)
			}
		})
	}
}

const linkA = "20260907-link-a-task"
const linkB = "20260907-link-b-task"
const linkD = "20260907-link-dir-task"

func linkMigrationFixture(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	root := filepath.Join(project, "kanban")
	for _, state := range States {
		if err := os.MkdirAll(filepath.Join(root, state), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(project, "README.md"), "project target")
	write(filepath.Join(filepath.Dir(root), "图 片.png"), "image target")
	legacyMigrationCard(t, root, "backlog", linkA, false, "# A\n- TYPE: Chore\n[说明](../../README.md?q=1#intro)\n[other](../done/"+linkB+".md#b)\n![图](<../../图 片.png>)\n[参考](../done/"+linkD+"/spec.md)\n")
	legacyMigrationCard(t, root, "done", linkB, false, "# B\n- TYPE: Chore\n[back](../backlog/"+linkA+".md)\n")
	directory := legacyMigrationCard(t, root, "done", linkD, true, "# D\n- TYPE: Chore\n- SIZE: large\n[old](../"+linkB+".md)\n[asset](notes.txt)\n")
	write(filepath.Join(filepath.Dir(directory), "notes.txt"), "attachment")
	write(filepath.Join(filepath.Dir(directory), "report.md"), "[旧卡][ref]\n\n[ref]: ../../backlog/"+linkA+".md#intro\n")
	// Repository files outside the board are never scanned or changed.
	write(filepath.Join(project, "outside.md"), "[old](kanban/done/"+linkB+".md)")
	return root
}

func assertLinkTargets(t *testing.T, root string) {
	t.Helper()
	docs := []string{filepath.Join(root, "backlog", linkA, "spec.md"), filepath.Join(root, "done", linkB, "spec.md"), filepath.Join(root, "done", linkD, "spec.md"), filepath.Join(root, "done", linkD, "report.md")}
	wants := [][]string{{filepath.Join(filepath.Dir(root), "README.md"), docs[1], filepath.Join(filepath.Dir(root), "图 片.png"), docs[2]}, {docs[0]}, {docs[1], filepath.Join(root, "done", linkD, "notes.txt")}, {docs[0]}}
	for i, doc := range docs {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		parsed := goldmark.DefaultParser().Parse(text.NewReader(data))
		index := 0
		_ = ast.Walk(parsed, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
			if !enter {
				return ast.WalkContinue, nil
			}
			var dest []byte
			switch x := n.(type) {
			case *ast.Link:
				dest = x.Destination
			case *ast.Image:
				dest = x.Destination
			default:
				return ast.WalkContinue, nil
			}
			u, err := url.Parse(markdownURL(dest))
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Clean(filepath.Join(filepath.Dir(doc), filepath.FromSlash(u.Path)))
			if index >= len(wants[i]) || migrationPathKey(target) != migrationPathKey(wants[i][index]) {
				t.Fatalf("%s destination %d = %s", doc, index, target)
			}
			if _, err := os.ReadFile(target); err != nil {
				t.Fatal(err)
			}
			index++
			return ast.WalkContinue, nil
		})
		if index != len(wants[i]) {
			t.Fatalf("lost link in %s", doc)
		}
	}
	original, err := os.ReadFile(filepath.Join(filepath.Dir(root), "outside.md"))
	if err != nil || string(original) != "[old](kanban/done/"+linkB+".md)" {
		t.Fatal("outside file changed")
	}
	before := map[string]time.Time{}
	contents := map[string]string{}
	for _, doc := range docs {
		info, _ := os.Stat(doc)
		before[doc] = info.ModTime()
		data, _ := os.ReadFile(doc)
		contents[doc] = string(data)
	}
	if n, err := MigrateCards(root, InitOptions{}); err != nil || n != 0 {
		t.Fatalf("repeat: %d %v", n, err)
	}
	for _, doc := range docs {
		info, _ := os.Stat(doc)
		data, _ := os.ReadFile(doc)
		if !info.ModTime().Equal(before[doc]) || string(data) != contents[doc] {
			t.Fatal("repeat changed content/mtime")
		}
	}
}

func TestMigrationPreservesActualLinkTargets(t *testing.T) {
	root := linkMigrationFixture(t)
	if n, err := MigrateCards(root, InitOptions{}); err != nil || n != 3 {
		t.Fatalf("migration %d %v", n, err)
	}
	assertLinkTargets(t, root)
}

func prepareLinkMigration(t *testing.T, root string) OperationRecord {
	t.Helper()
	if err := ensureControl(root); err != nil {
		t.Fatal(err)
	}
	b, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := planMigration(root, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOperation(root, control(root, "operations", r.ID+".json"), r, false); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMigrationLinkRecovery(t *testing.T) {
	stages := append([]string{"migration-link-file", "migration-links"}, migrationStages...)
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			root := linkMigrationFixture(t)
			r := prepareLinkMigration(t, root)
			stop := errors.New("stop")
			err := applyRecordWithCheckpoint(root, control(root, "operations", r.ID+".json"), &r, func(at string) error {
				if at == stage {
					return stop
				}
				return nil
			})
			if !errors.Is(err, stop) {
				t.Fatal(err)
			}
			if _, err := MigrateCards(root, InitOptions{}); err != nil {
				t.Fatal(err)
			}
			assertLinkTargets(t, root)
		})
	}
}

func TestMigrationLinkKillRestart(t *testing.T) {
	for _, stage := range append([]string{"migration-link-file", "migration-links"}, migrationStages...) {
		t.Run(stage, func(t *testing.T) {
			root := linkMigrationFixture(t)
			prepareLinkMigration(t, root)
			cmd := exec.Command(os.Args[0], "-test.run=^TestMigrationCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_TEST_MIGRATION_ROOT="+root, "KANDER_TEST_MIGRATION_STAGE="+stage)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			ready := make(chan bool, 1)
			go func() { s := bufio.NewScanner(stdout); ready <- s.Scan() && s.Text() == "crash-boundary" }()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child did not reach checkpoint")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("checkpoint timeout")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			if _, err := MigrateCards(root, InitOptions{}); err != nil {
				t.Fatal(err)
			}
			assertLinkTargets(t, root)
		})
	}
}

func TestMigrationRejectsUnsupportedLinksBeforeWriting(t *testing.T) {
	for _, syntax := range []string{`<img src="relative.png">`, `<img srcset="https://example.com/a.png 1x, local.png 2x">`, `[x](bad%ZZ.md)`, `[x](dir\file.md)`, `[[relative.md]]`} {
		t.Run(syntax, func(t *testing.T) {
			root := tempBoard(t)
			body := "# Card\n- TYPE: Chore\n" + syntax + "\n"
			path := legacyMigrationCard(t, root, "backlog", linkA, false, body)
			if _, err := MigrateCards(root, InitOptions{}); err == nil {
				t.Fatal("unsupported syntax silently migrated")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != body {
				t.Fatal("preflight changed source")
			}
			records, err := operationRecords(root)
			if err != nil || len(records) != 0 {
				t.Fatal("preflight published a partial plan")
			}
		})
	}
}

func TestMigrationRejectsTamperedLinkAfterImage(t *testing.T) {
	root := linkMigrationFixture(t)
	r := prepareLinkMigration(t, root)
	r.Migrations[0].After = strings.Replace(r.Migrations[0].After, "README.md", "other.md", 1)
	if err := validateRecord(root, &r); err == nil {
		t.Fatal("arbitrary link edit accepted")
	}
	r = prepareLinkMigration(t, root)
	r.Files[0].After += "unauthorized body change"
	if err := validateRecord(root, &r); err == nil {
		t.Fatal("arbitrary directory body edit accepted")
	}
}

func TestMigrationRecoveryAfterSecondCardPublishes(t *testing.T) {
	root := linkMigrationFixture(t)
	r := prepareLinkMigration(t, root)
	stop := errors.New("second card published")
	published := 0
	err := applyRecordWithCheckpoint(root, control(root, "operations", r.ID+".json"), &r, func(at string) error {
		if at == "migration-publish" {
			published++
			if published == 2 {
				return stop
			}
		}
		return nil
	})
	if !errors.Is(err, stop) || published != 2 {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	assertLinkTargets(t, root)
}
