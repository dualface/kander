//go:build windows

package fs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

const chdirHelperEnv = "KANDER_FS_CHDIR_HELPER"

// TestWindowsTempDirChdirHelper is the child process of the chdir test: it enters the directory named by the
// environment variable, which fails with a sharing violation when the lease holds DELETE.
func TestWindowsTempDirChdirHelper(t *testing.T) {
	dir := os.Getenv(chdirHelperEnv)
	if dir == "" {
		t.Skip("helper process only")
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
}

func TestWindowsPrivateTempDirAllowsChdirFromAnotherProcess(t *testing.T) {
	dir, err := CreatePrivateTempDir(t.TempDir(), "chdir.")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsTempDirChdirHelper$")
	cmd.Env = append(os.Environ(), chdirHelperEnv+"="+dir.Path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child chdir failed: %v\n%s", err, out)
	}
}

func TestWindowsPrivateTempDirLeaseBlocksRenameAndDelete(t *testing.T) {
	parent := t.TempDir()
	dir, err := CreatePrivateTempDir(parent, "lease.")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := os.Rename(dir.Path, filepath.Join(parent, "renamed")); err == nil {
		t.Fatal("rename must fail while the lease is open")
	}
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(dir.Path),
		windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err == nil {
		windows.CloseHandle(handle)
		t.Fatal("DELETE open must fail while the lease is open")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("DELETE open error = %v, want sharing violation", err)
	}
	if _, err := os.Stat(dir.Path); err != nil {
		t.Fatalf("directory must still exist: %v", err)
	}
}

func TestWindowsPrivateTempDirCloseRemovesDirectory(t *testing.T) {
	dir, err := CreatePrivateTempDir(t.TempDir(), "close.")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTextAtomic(dir.Path, filepath.Join(dir.Path, "a.txt"), "a\n", false); err != nil {
		t.Fatal(err)
	}
	if err := CreatePrivateDirectory(dir.Path, filepath.Join(dir.Path, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := WriteTextAtomic(dir.Path, filepath.Join(dir.Path, "sub", "b.txt"), "b\n", false); err != nil {
		t.Fatal(err)
	}
	if err := dir.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir.Path); !os.IsNotExist(err) {
		t.Fatalf("directory must be removed, stat err = %v", err)
	}
}

func TestWindowsPrivateTempDirCloseFailsClosedOnReplacement(t *testing.T) {
	parent := t.TempDir()
	dir, err := CreatePrivateTempDir(parent, "swap.")
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(parent, "moved")
	tempDirBeforeReopen = func(path string) {
		if err := os.Rename(path, moved); err != nil {
			t.Errorf("rename: %v", err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Errorf("mkdir: %v", err)
		}
	}
	t.Cleanup(func() { tempDirBeforeReopen = nil })
	err = dir.Close()
	if err == nil || !errors.Is(err, ErrTempCleanup) {
		t.Fatalf("close error = %v, want ErrTempCleanup", err)
	}
	if _, err := os.Stat(dir.Path); err != nil {
		t.Fatalf("replacement directory must not be deleted: %v", err)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("original directory must remain: %v", err)
	}
}

func TestWindowsGrantLocalGroupReadRejectsBroadGroups(t *testing.T) {
	dir, err := CreatePrivateTempDir(t.TempDir(), "grant.")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	before := daclString(t, dir.Path)
	for _, group := range []string{"Users", "Everyone", "Authenticated Users", `BUILTIN\Users`, ""} {
		if err := dir.GrantLocalGroupRead([]string{group}); err == nil {
			t.Fatalf("group %q must be rejected", group)
		}
	}
	if err := dir.GrantLocalGroupRead([]string{"KanderNoSuchGroup7f3a9c"}); err != nil {
		t.Fatalf("unknown group must be skipped: %v", err)
	}
	if after := daclString(t, dir.Path); after != before {
		t.Fatalf("DACL changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestWindowsMachineDomainSIDShape(t *testing.T) {
	cases := map[string]bool{
		"S-1-5-21-1380200320-1560425796-478443099-1002": true,
		"S-1-5-32-545":   false,
		"S-1-1-0":        false,
		"S-1-5-11":       false,
		"S-1-5-21-1-2-3": false,
	}
	for text, want := range cases {
		sid, err := windows.StringToSid(text)
		if err != nil {
			t.Fatal(err)
		}
		if got := isMachineDomainSID(sid); got != want {
			t.Fatalf("%s: got %v want %v", text, got, want)
		}
	}
}

// The grant walk adds a read entry to the directory and every entry inside it, keeps the protected DACL and the
// current user's full control, and gives the directory entry inheritance for later objects.
func TestWindowsGrantReadWalkAddsEntries(t *testing.T) {
	dir, err := CreatePrivateTempDir(t.TempDir(), "walk.")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	file := filepath.Join(dir.Path, "prompt.txt")
	if err := WriteTextAtomic(dir.Path, file, "p\n", false); err != nil {
		t.Fatal(err)
	}
	if err := MakeRegularFileReadOnly(dir.Path, file); err != nil {
		t.Fatal(err)
	}
	if err := CreatePrivateDirectory(dir.Path, filepath.Join(dir.Path, "nested")); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(dir.Path, "nested", "x.md")
	if err := WriteTextAtomic(dir.Path, nestedFile, "x\n", false); err != nil {
		t.Fatal(err)
	}
	// A builtin SID stands in for the local group so the test does not need to create accounts; resolution is
	// covered separately.
	sid, err := windows.StringToSid("S-1-5-32-545")
	if err != nil {
		t.Fatal(err)
	}
	impl := dir.impl.(*windowsTempDir)
	if err := grantReadWin(impl.handle, impl.path, []*windows.SID{sid}, true, 0, new(int)); err != nil {
		t.Fatal(err)
	}
	user := currentUserSID(t)
	for _, path := range []string{dir.Path, file, filepath.Join(dir.Path, "nested"), nestedFile} {
		sddl := daclString(t, path)
		if !strings.HasPrefix(sddl, "D:P") {
			t.Fatalf("%s: DACL must stay protected: %s", path, sddl)
		}
		if !strings.Contains(sddl, ";BU)") {
			t.Fatalf("%s: read entry missing: %s", path, sddl)
		}
		if !strings.Contains(sddl, ";FA;;;"+user+")") {
			t.Fatalf("%s: user full control missing: %s", path, sddl)
		}
	}
	if sddl := daclString(t, dir.Path); !strings.Contains(sddl, "(A;OICI;0x1200a9;;;BU)") {
		t.Fatalf("directory entry must be inheritable read-execute: %s", sddl)
	}
	if sddl := daclString(t, file); !strings.Contains(sddl, "(A;;0x1200a9;;;BU)") {
		t.Fatalf("file entry must be non-inheritable read-execute: %s", sddl)
	}
}

// When this machine has the codex sandbox group, resolution accepts it end to end.
func TestWindowsGrantLocalGroupReadCodexSandboxUsers(t *testing.T) {
	if _, _, _, err := windows.LookupSID("", "CodexSandboxUsers"); err != nil {
		t.Skip("CodexSandboxUsers is not present on this machine")
	}
	dir, err := CreatePrivateTempDir(t.TempDir(), "codex.")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := dir.GrantLocalGroupRead([]string{"CodexSandboxUsers"}); err != nil {
		t.Fatal(err)
	}
	sid, _, _, _ := windows.LookupSID("", "CodexSandboxUsers")
	if sddl := daclString(t, dir.Path); !strings.Contains(sddl, "(A;OICI;0x1200a9;;;"+sid.String()+")") {
		t.Fatalf("group entry missing: %s", sddl)
	}
}

func daclString(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func currentUserSID(t *testing.T) string {
	t.Helper()
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid.String()
}
