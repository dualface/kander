package fs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExclusiveLockBlocksAnotherProcess(t *testing.T) {
	if os.Getenv("KANDER_FS_LOCK_HELPER") == "1" {
		lockHelper()
		os.Exit(0)
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lock, err := LockExclusive(f)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "acquired")
	cmd := exec.Command(os.Args[0], "-test.run=TestExclusiveLockBlocksAnotherProcess", "-test.v=false")
	cmd.Env = append(os.Environ(),
		"KANDER_FS_LOCK_HELPER=1",
		"KANDER_FS_LOCK_PATH="+lockPath,
		"KANDER_FS_LOCK_MARKER="+marker,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		t.Fatal("exclusive lock did not block")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("helper acquired lock while parent still holds it")
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "acquired" {
		t.Fatalf("marker=%q", data)
	}
}

func lockHelper() {
	path := os.Getenv("KANDER_FS_LOCK_PATH")
	marker := os.Getenv("KANDER_FS_LOCK_MARKER")
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
	lock, err := LockExclusive(f)
	if err != nil {
		os.Exit(3)
	}
	_ = os.WriteFile(marker, []byte("acquired"), 0o600)
	_ = lock.Unlock()
	os.Exit(0)
}

func TestPrivatePermissionHelpers(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(private, "secret.json")
	if err := os.WriteFile(file, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := TightenPrivateDirectory(private); err != nil {
		t.Fatal(err)
	}
	if err := TightenPrivateFile(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(private)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode=%o", st.Mode().Perm())
		}
		st, err = os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("file mode=%o", st.Mode().Perm())
		}
	}
}

func TestPosixAtomicCreateIsPrivateAndRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX no-follow backend only")
	}
	base := t.TempDir()
	root := filepath.Join(base, "board")
	state := filepath.Join(root, "backlog")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(state, "20260823-private-task.md")
	if err := WriteTextAtomic(root, document, "private\n", false); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(document)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", st.Mode().Perm())
	}
	rootLink := filepath.Join(base, "board-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	_, err = ReadRegularFile(rootLink, filepath.Join(rootLink, "backlog", filepath.Base(document)))
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("expected unsafe, got %v", err)
	}
}

func TestOpenWritableRegularFileAndMakeReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.log")
	if err := WriteTextAtomic(root, path, "old\n", false); err != nil {
		t.Fatal(err)
	}
	f, err := OpenWritableRegularFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if _, err := f.WriteString("new\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("data=%q", data)
	}
	if err := MakeRegularFileReadOnly(root, path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o400 {
			t.Fatalf("mode=%o", st.Mode().Perm())
		}
	}
}

func TestRemoveRegularFileIfExists(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "prefs.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveRegularFileIfExists(root, path)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	removed, err = RemoveRegularFileIfExists(root, path)
	if err != nil || removed {
		t.Fatalf("second removed=%v err=%v", removed, err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveRegularFileIfExists(root, link); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("expected unsafe symlink error, got %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target was removed: %v", err)
	}
}

func TestPosixNeutralAppendAndInheritedDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX no-follow backend only")
	}
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(gitDir, "info")
	old := unixUmask(0o022)
	defer unixUmask(old)
	if err := EnsureInheritedDirectoryPath(info); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(info)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("inherited dir mode=%o", st.Mode().Perm())
	}
	exclude := filepath.Join(info, "exclude")
	if err := AppendUniqueLine(gitDir, exclude, "/kanban/"); err != nil {
		t.Fatal(err)
	}
	if err := AppendUniqueLine(gitDir, exclude, "/kanban/"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "/kanban/\n" {
		t.Fatalf("exclude=%q", data)
	}
	st, err = os.Stat(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("exclude mode=%o", st.Mode().Perm())
	}
}

func TestPosixPrivateDirectoryCreateIsStrict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX no-follow backend only")
	}
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := CreatePrivateDirectory(root, private); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(private)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode=%o", st.Mode().Perm())
	}
	if err := CreatePrivateDirectory(root, private); err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected exist, got %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := EnsurePrivateDirectory(root, nested, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected not exist, got %v", err)
	}
	if err := EnsurePrivateDirectory(root, nested, true); err != nil {
		t.Fatal(err)
	}
	st, err = os.Stat(nested)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("nested mode=%o", st.Mode().Perm())
	}
}

func TestPosixPrivateRuntimeCleanupNeverFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX no-follow backend only")
	}
	base := t.TempDir()
	parent := filepath.Join(base, "runtime-parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim.txt")
	if err := os.WriteFile(victim, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	temp, err := CreatePrivateTempDir(parent, "codex-review.")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(temp.Path, "planted-link")); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(temp.Path, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "inside.txt"), []byte("inside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(temp.Path, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(temp.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp still exists: %v", err)
	}
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside\n" {
		t.Fatalf("victim=%q", data)
	}
}

func TestReadWriteRenameAndEscape(t *testing.T) {
	root := t.TempDir()
	todo := filepath.Join(root, "todo")
	working := filepath.Join(root, "working")
	if err := os.Mkdir(todo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(working, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(todo, "20260823-safe-task.md")
	if err := os.WriteFile(source, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFile(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\n" {
		t.Fatalf("read=%q", data)
	}
	got, ok, err := ReadRegularFileIfExists(root, source)
	if err != nil || !ok || string(got) != "old\n" {
		t.Fatalf("if-exists %q %v %v", got, ok, err)
	}
	missing, ok, err := ReadRegularFileIfExists(root, filepath.Join(todo, "missing.md"))
	if err != nil || ok || missing != nil {
		t.Fatalf("missing %v %v %v", missing, ok, err)
	}
	if err := WriteTextAtomic(root, source, "new\n", true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(working, filepath.Base(source))
	if err := Rename(root, source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
	data, err = ReadRegularFile(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("moved=%q", data)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteTextAtomic(root, outside, "x", true); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("escape write: %v", err)
	}
}

func TestCreateDirectoryWithTextFileRollbackKeepsSibling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX rollback path")
	}
	root := t.TempDir()
	working := filepath.Join(root, "working")
	if err := os.Mkdir(working, 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(working, "spec.md")
	if err := os.WriteFile(sibling, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(working, "20260823-rollback-task")
	longName := strings.Repeat("a", 240)
	if err := CreateDirectoryWithTextFile(root, dir, longName, "x\n"); err == nil {
		t.Fatal("expected write failure from overlong temp name")
	}
	data, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep\n" {
		t.Fatalf("sibling mutated: %q", data)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed dir left behind: %v", err)
	}
}

func TestCreateDirectoryWithTextFile(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "working")
	if err := os.Mkdir(working, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(working, "20260823-large-task")
	if err := CreateDirectoryWithTextFile(root, dir, "spec.md", "# spec\n"); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFile(root, filepath.Join(dir, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# spec\n" {
		t.Fatalf("spec=%q", data)
	}
	entries, err := ListDirectory(root, working)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "20260823-large-task" || entries[0].Kind != KindDirectory {
		t.Fatalf("entries=%v", entries)
	}
}

func TestIsReparsePointSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by junction tests on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "t")
	link := filepath.Join(dir, "l")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if !IsReparsePoint(link) {
		t.Fatal("expected symlink")
	}
	if IsReparsePoint(target) {
		t.Fatal("plain dir")
	}
	if IsReparsePoint(filepath.Join(dir, "missing")) {
		t.Fatal("missing")
	}
}

// The creator keeps a handle on the private temporary directory the whole time. On Windows that handle's share mode must allow
// later writes and deletes, otherwise the review runtime can neither write nor read back files in its own directory.
func TestPrivateTempDirSupportsProtectedIO(t *testing.T) {
	temp, err := CreatePrivateTempDir(t.TempDir(), "codex-review.")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := temp.Close(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	evidence := filepath.Join(temp.Path, "evidence.txt")
	errorLog := filepath.Join(temp.Path, "error.log")
	for _, path := range []string{evidence, errorLog} {
		if err := WriteTextAtomic(temp.Path, path, "body\n", false); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	stream, err := OpenWritableRegularFile(temp.Path, errorLog)
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	defer stream.Close()
	data, err := ReadRegularFile(temp.Path, evidence)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "body\n" {
		t.Fatalf("data=%q", data)
	}
	// The finalAccess of ListDirectory includes FILE_LIST_DIRECTORY, which really does trigger the sharing check on the directory leaf.
	entries, err := ListDirectory(temp.Path, temp.Path)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%v", entries)
	}
	if _, err := RemoveRegularFileIfExists(temp.Path, evidence); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestWriteExecutableAtomicInherited(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "kander")
	if err := WriteExecutableAtomicInherited(root, path, []byte("#!/bin/sh\n"), false); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		t.Fatalf("missing execute bits: %o", st.Mode().Perm())
	}
	if err := WriteExecutableAtomicInherited(root, path, []byte("#!/bin/sh\necho 1\n"), true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "#!/bin/sh\necho 1\n" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestCreateRelativeSymlinkAndRemoveNonDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "KANDER-AGENTS.md")
	if err := WriteBytesAtomicInherited(root, target, []byte("# rules\n"), false); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "AGENTS.md")
	if err := CreateRelativeSymlink(root, link, "KANDER-AGENTS.md"); err != nil {
		if runtime.GOOS == "windows" {
			if err := CreateRelativeHardLink(root, link, "KANDER-AGENTS.md"); err != nil {
				t.Skipf("symlink and hardlink unavailable: %v", err)
			}
		} else {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(link)
	if err != nil || string(data) != "# rules\n" {
		t.Fatalf("link data=%q err=%v", data, err)
	}
	if err := CreateRelativeSymlink(root, link, "KANDER-AGENTS.md"); err == nil {
		t.Fatal("existing link must fail")
	}
	ok, err := RemoveNonDirectoryIfExists(root, link)
	if err != nil || !ok {
		t.Fatalf("remove link: ok=%v err=%v", ok, err)
	}
	ok, err = RemoveNonDirectoryIfExists(root, filepath.Join(root, "missing"))
	if err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}
