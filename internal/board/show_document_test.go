package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

const planText = "# 计划\n\n- 第一步\n- 最后一行没有换行"

// documentCard creates a large directory card in backlog with a plan.md.
func documentCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	resetLang(t)
	s := transactionCard(t, root, slug, true)
	writeCardFile(t, s.Entry, "plan.md", planText)
	return s
}

func writeCardFile(t *testing.T, entry Entry, name, text string) string {
	t.Helper()
	path := filepath.Join(entry.Path, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// showDoc reads one document of a card of the current test board and reports
// the result the way a command would: exit code, document text, error line.
func showDoc(t *testing.T, path, id string) (int, string, string) {
	t.Helper()
	root, err := BoardRoot()
	if err != nil {
		t.Fatal(err)
	}
	_, text, err := ReadDocumentSnapshot(root, id, path)
	if err != nil {
		return 1, "", "kander: " + err.Error() + "\n"
	}
	return 0, text, ""
}

func documentError(t *testing.T, root, id, name string) *Error {
	t.Helper()
	_, _, err := ReadDocumentSnapshot(root, id, name)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("read %q: err=%v, want a board error", name, err)
	}
	return e
}

// msg renders a message without being shadowed by the testing parameter t.
func msg(id string, args ...any) string { return t(id, args...) }

func TestShowDocumentRejectsInvalidPaths(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "invalid-path")
	for name, path := range map[string]string{
		"empty":            "",
		"absolute":         "/etc/passwd",
		"parent segment":   "sub/../plan.md",
		"dot segment":      "./plan.md",
		"backslash":        `sub\plan.md`,
		"colon":            "sub:plan.md",
		"hidden segment":   ".hidden/plan.md",
		"trailing space":   "sub /plan.md",
		"trailing dot":     "sub./plan.md",
		"spec case folded": "SPEC.MD",
	} {
		t.Run(name, func(t *testing.T) {
			if e := documentError(t, root, s.Entry.TaskID, path); e.Code != "board.transaction_invalid" {
				t.Fatalf("code=%q", e.Code)
			}
			code, out, stderr := showDoc(t, path, s.Entry.TaskID)
			if code == 0 || out != "" || !strings.Contains(stderr, msg("board.transaction_invalid", path)) {
				t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
			}
		})
	}
}

func TestShowDocumentRefusesSymlinks(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "symlinks")
	outside := t.TempDir()
	const secret = "outside-secret"
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(s.Entry.Path, "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(s.Entry.Path, "linkdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for name, path := range map[string]string{"file": "link.md", "parent": "linkdir/secret.md"} {
		t.Run(name, func(t *testing.T) {
			code, out, stderr := showDoc(t, path, s.Entry.TaskID)
			if code == 0 || strings.Contains(out+stderr, secret) {
				t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
			}
			if _, _, err := ReadDocumentSnapshot(root, s.Entry.TaskID, path); err == nil {
				t.Fatal("symlink read")
			}
		})
	}
}

func TestShowDocumentMissingAndDirectoryTargets(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "missing-doc")
	if err := os.Mkdir(filepath.Join(s.Entry.Path, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if e := documentError(t, root, s.Entry.TaskID, "absent.md"); e.Code != "board.document_not_found" || !strings.Contains(e.Message, "absent.md") {
		t.Fatalf("missing: %+v", e)
	}
	if e := documentError(t, root, s.Entry.TaskID, "no/such/dir.md"); e.Code != "board.document_not_found" {
		t.Fatalf("missing parent: %+v", e)
	}
	code, out, stderr := showDoc(t, "absent.md", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, msg("board.document_not_found", "absent.md")) {
		t.Fatalf("missing: code=%d out=%q stderr=%q", code, out, stderr)
	}
	code, out, stderr = showDoc(t, "notes", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, "notes") {
		t.Fatalf("directory: code=%d out=%q stderr=%q", code, out, stderr)
	}
}

func TestShowDocumentTranslatesMissingDocument(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "translated")
	t.Cleanup(func() { resetLang(t) })
	for lang, want := range map[string]string{"en": "Document not found in the card: nope.md", "cn": "卡片中没有该文档: nope.md", "ja": "カードにその文書がありません: nope.md"} {
		t.Run(lang, func(t *testing.T) {
			t.Setenv(config.EnvLang, lang)
			config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
			code, _, stderr := showDoc(t, "nope.md", s.Entry.TaskID)
			if code == 0 || !strings.Contains(stderr, want) || strings.Contains(stderr, "board.document_not_found") {
				t.Fatalf("code=%d stderr=%q, want %q", code, stderr, want)
			}
		})
	}
}

func TestShowDocumentRejectsInvalidUTF8(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "bad-utf8")
	writeCardFile(t, s.Entry, "binary.md", "ok\xff\xfe")
	if e := documentError(t, root, s.Entry.TaskID, "binary.md"); e.Code != "board.task_document_is_not_valid_utf_8" {
		t.Fatalf("code=%q", e.Code)
	}
	code, out, stderr := showDoc(t, "binary.md", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, msg("board.task_document_is_not_valid_utf_8", "binary.md")) {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
	}
}

func listCardFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, rel+"|"+info.Mode().String()+"|"+info.ModTime().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestShowDocumentIsReadOnly(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "read-only")
	before := listCardFiles(t, root)
	for _, path := range []string{"plan.md", "spec.md"} {
		if code, _, stderr := showDoc(t, path, s.Entry.TaskID); code != 0 {
			t.Fatal(stderr)
		}
	}
	if after := listCardFiles(t, root); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("board changed:\nbefore=%v\nafter=%v", before, after)
	}
	if again := transactionSnapshot(t, root, s.Entry.TaskID); again.Revision != s.Revision {
		t.Fatalf("revision %d -> %d", s.Revision, again.Revision)
	}
}

func TestShowDocumentInEveryState(t *testing.T) {
	root := tempBoard(t)
	for _, state := range States {
		t.Run(state, func(t *testing.T) {
			s := documentCard(t, root, "state-"+state)
			if state != "backlog" {
				if err := os.Rename(s.Entry.Path, filepath.Join(root, state, s.Entry.TaskID)); err != nil {
					t.Fatal(err)
				}
			}
			code, out, stderr := showDoc(t, "plan.md", s.Entry.TaskID)
			if code != 0 || out != planText {
				t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
			}
		})
	}
}

func TestShowDocumentReadsManagedDirectoriesButUpdateRefusesThem(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "managed-dirs")
	writeCardFile(t, s.Entry, "reviews/run-1/report.md", "review report")
	writeCardFile(t, s.Entry, "dispatches/d-1.json", `{"id":"d-1"}`)
	for path, want := range map[string]string{"reviews/run-1/report.md": "review report", "dispatches/d-1.json": `{"id":"d-1"}`} {
		code, out, stderr := showDoc(t, path, s.Entry.TaskID)
		if code != 0 || out != want {
			t.Fatalf("%s: code=%d out=%q stderr=%q", path, code, out, stderr)
		}
		err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: path, Text: "x", ExpectedRevision: s.Revision})
		var e *Error
		if !errors.As(err, &e) || e.Code != "board.transaction_managed" {
			t.Fatalf("%s: update err=%v", path, err)
		}
	}
}

func TestShowDocumentOnLegacyFileCard(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "legacy-file", false)
	if err := os.RemoveAll(s.Entry.Path); err != nil {
		t.Fatal(err)
	}
	legacyMigrationCard(t, root, "backlog", s.Entry.TaskID, false, s.Text)
	code, out, stderr := showDoc(t, "spec.md", s.Entry.TaskID)
	if code != 0 || out != s.Text {
		t.Fatalf("spec: code=%d stderr=%q", code, stderr)
	}
	if e := documentError(t, root, s.Entry.TaskID, "plan.md"); e.Code != "board.transaction_directory_required" {
		t.Fatalf("code=%q", e.Code)
	}
	code, out, stderr = showDoc(t, "plan.md", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, msg("board.transaction_directory_required")) {
		t.Fatalf("plan: code=%d out=%q stderr=%q", code, out, stderr)
	}
}

func TestReadDocumentSnapshotAcceptsTaskFileName(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "file-name")
	plain, text, err := ReadDocumentSnapshot(root, s.Entry.TaskID, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	named, namedText, err := ReadDocumentSnapshot(root, s.Entry.TaskID+".md", "plan.md")
	if err != nil || named.Revision != plain.Revision || namedText != text {
		t.Fatalf("named=%+v text=%q err=%v", named, namedText, err)
	}
}

// The revision and the text of one read come from the same locked state: a writer
// that starts between the snapshot and the document read waits for the read.
func TestReadDocumentSnapshotIsOneTransaction(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "one-transaction")
	updated := make(chan error, 1)
	afterDocumentSnapshot = func() {
		go func() {
			updated <- UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "plan.md", Text: "after", ExpectedRevision: s.Revision})
		}()
		select {
		case err := <-updated:
			t.Errorf("the write committed before the read returned: %v", err)
			updated <- err
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Cleanup(func() { afterDocumentSnapshot = nil })
	snapshot, text, err := ReadDocumentSnapshot(root, s.Entry.TaskID, "plan.md")
	if err != nil || text != planText || snapshot.Revision != s.Revision {
		t.Fatalf("text=%q revision=%d err=%v, want the state before the write", text, snapshot.Revision, err)
	}
	afterDocumentSnapshot = nil
	select {
	case err = <-updated:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the write never finished")
	}
	snapshot, text, err = ReadDocumentSnapshot(root, s.Entry.TaskID, "plan.md")
	if err != nil || text != "after" || snapshot.Revision <= s.Revision {
		t.Fatalf("text=%q revision=%d err=%v, want the written state", text, snapshot.Revision, err)
	}
}
