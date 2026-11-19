package board

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

func showDocument(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return capture(t, func() int { return RunShow(args) })
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

func TestShowDocumentPrintsOriginalText(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "print-doc")
	code, out, stderr := showDocument(t, "--document", "plan.md", s.Entry.TaskID)
	if code != 0 || stderr != "" || out != planText {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
	}
	code, out, stderr = showDocument(t, s.Entry.TaskID, "--document", "spec.md")
	if code != 0 || stderr != "" || out != s.Text {
		t.Fatalf("spec: code=%d stderr=%q equal=%v", code, stderr, out == s.Text)
	}
	_, jsonOut, _ := showDocument(t, "--json", s.Entry.TaskID)
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(jsonOut), &snapshot); err != nil || snapshot.Text != out {
		t.Fatalf("spec text differs from show --json: %v", err)
	}
}

func TestShowDocumentJSONFields(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "json-doc")
	code, out, stderr := showDocument(t, "--json", "--document", "plan.md", s.Entry.TaskID)
	if code != 0 || stderr != "" || !strings.HasSuffix(out, "}\n") {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 4 {
		t.Fatalf("fields=%v, want path, text, revision, entry", doc)
	}
	var path, text string
	var revision uint64
	for name, target := range map[string]any{"path": &path, "text": &text, "revision": &revision} {
		if err := json.Unmarshal(doc[name], target); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if path != "plan.md" || text != planText {
		t.Fatalf("path=%q text=%q", path, text)
	}
	_, snapshotOut, _ := showDocument(t, "--json", s.Entry.TaskID)
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(snapshotOut), &snapshot); err != nil {
		t.Fatal(err)
	}
	if string(doc["revision"]) != string(snapshot["revision"]) || revision == 0 {
		t.Fatalf("revision %s differs from show --json %s", doc["revision"], snapshot["revision"])
	}
	if string(doc["entry"]) != string(snapshot["entry"]) {
		t.Fatalf("entry %s differs from show --json %s", doc["entry"], snapshot["entry"])
	}
}

func TestShowWithoutDocumentIsUnchanged(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "plain-show")
	code, out, _ := showDocument(t, s.Entry.TaskID)
	want := msg("board.show_location", s.Entry.State, s.Entry.Path) + "\n\n" + s.Text
	if code != 0 || out != want {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = showDocument(t, "--json", s.Entry.TaskID)
	var got Snapshot
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Text != s.Text || got.Revision != s.Revision || got.Entry.Path != s.Entry.Path {
		t.Fatalf("json: code=%d out=%q", code, out)
	}
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
			code, out, stderr := showDocument(t, "--document", path, s.Entry.TaskID)
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
			code, out, stderr := showDocument(t, "--document", path, s.Entry.TaskID)
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
	code, out, stderr := showDocument(t, "--document", "absent.md", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, msg("board.document_not_found", "absent.md")) {
		t.Fatalf("missing: code=%d out=%q stderr=%q", code, out, stderr)
	}
	code, out, stderr = showDocument(t, "--document", "notes", s.Entry.TaskID)
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
			code, _, stderr := showDocument(t, "--document", "nope.md", s.Entry.TaskID)
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
	code, out, stderr := showDocument(t, "--document", "binary.md", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, msg("board.task_document_is_not_valid_utf_8", "binary.md")) {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
	}
}

// listCardFiles snapshots every entry below dir: mode and modification time,
// plus size and SHA-256 for regular files. It reads each object with os.Lstat
// because on Windows DirEntry.Info returns the copy stored in the parent
// directory listing, which NTFS refreshes lazily, so a directory time can
// change between two snapshots without any write.
func listCardFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		line := rel + "|" + info.Mode().String() + "|" + info.ModTime().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += fmt.Sprintf("|%d|%x", info.Size(), sha256.Sum256(data))
		}
		files = append(files, line)
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
	for _, args := range [][]string{{"--document", "plan.md"}, {"--json", "--document", "spec.md"}} {
		if code, _, stderr := showDocument(t, append(args, s.Entry.TaskID)...); code != 0 {
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
			code, out, stderr := showDocument(t, "--document", "plan.md", s.Entry.TaskID)
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
		code, out, stderr := showDocument(t, "--document", path, s.Entry.TaskID)
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
	code, out, stderr := showDocument(t, "--document", "spec.md", s.Entry.TaskID)
	if code != 0 || out != s.Text {
		t.Fatalf("spec: code=%d stderr=%q", code, stderr)
	}
	if e := documentError(t, root, s.Entry.TaskID, "plan.md"); e.Code != "board.transaction_directory_required" {
		t.Fatalf("code=%q", e.Code)
	}
	code, out, stderr = showDocument(t, "--document", "plan.md", s.Entry.TaskID)
	if code == 0 || out != "" || !strings.Contains(stderr, msg("board.transaction_directory_required")) {
		t.Fatalf("plan: code=%d out=%q stderr=%q", code, out, stderr)
	}
}

func TestShowDocumentUsageErrors(t *testing.T) {
	root := tempBoard(t)
	s := documentCard(t, root, "usage")
	id := s.Entry.TaskID
	for name, args := range map[string][]string{
		"missing value":    {id, "--document"},
		"duplicate":        {"--document", "plan.md", "--document", "plan.md", id},
		"missing task id":  {"--document", "plan.md"},
		"multiple task id": {"--document", "plan.md", id, id},
	} {
		t.Run(name, func(t *testing.T) {
			if code, out, _ := showDocument(t, args...); code != 2 || out != "" {
				t.Fatalf("code=%d out=%q", code, out)
			}
		})
	}
	t.Run("duplicate names the option", func(t *testing.T) {
		_, _, stderr := showDocument(t, "--document", "a.md", "--document", "b.md", id)
		if !strings.Contains(stderr, msg("board.option_given_twice", "--document")) {
			t.Fatalf("stderr=%q", stderr)
		}
	})
	t.Run("empty value is a path error", func(t *testing.T) {
		code, out, stderr := showDocument(t, "--document", "", id)
		if code != 1 || out != "" || !strings.Contains(stderr, msg("board.transaction_invalid", "")) {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	})
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
