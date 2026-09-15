package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestPathUsesFirstExecutableIdentity(t *testing.T) {
	for _, kind := range []string{"direct", "symlink", "hardlink", "missing", "shadowed", "non-executable", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			source := stubBinary(t)
			first, second := t.TempDir(), filepath.Dir(source)
			entry := filepath.Join(first, binaryName())
			match := false
			switch kind {
			case "direct":
				first = second
				match = true
			case "symlink", "hardlink":
				link := os.Symlink
				if kind == "hardlink" {
					link = os.Link
				}
				if err := link(source, entry); err != nil {
					t.Skipf("link unavailable: %v", err)
				}
				match = true
			case "shadowed", "non-executable":
				mode := os.FileMode(0o755)
				if kind == "non-executable" {
					if runtime.GOOS == "windows" {
						t.Skip("Windows uses PATHEXT rather than executable mode bits")
					}
					mode, match = 0o644, true
				}
				if err := os.WriteFile(entry, []byte("other binary"), mode); err != nil {
					t.Fatal(err)
				}
			case "missing":
				second = t.TempDir()
			case "unknown":
				source = filepath.Join(t.TempDir(), "missing")
			}
			t.Setenv("PATH", first+string(os.PathListSeparator)+second)
			check := inspectPath(source)
			if check.Matches != match {
				t.Fatalf("check=%+v", check)
			}
			if kind == "shadowed" && check.Found != entry {
				t.Fatalf("looked past the first match: %+v", check)
			}
			if kind == "unknown" && check.Err == nil || kind == "missing" && check.Found != "" {
				t.Fatalf("check=%+v", check)
			}
		})
	}
}

func TestMatchingPathDoesNotPrompt(t *testing.T) {
	source := stubBinary(t)
	mockCurrentBinary(t, source)
	t.Setenv("PATH", filepath.Dir(source))
	if offerCopy() {
		t.Fatal("matching PATH still requested a copy")
	}
}

func TestOfferCopyAutoWhenPathMismatches(t *testing.T) {
	source := stubBinary(t)
	mockCurrentBinary(t, source)
	t.Setenv("PATH", t.TempDir())
	if !offerCopy() {
		t.Fatal("mismatched PATH skipped the copy")
	}
}

func TestGlobalInstallDefaultsCopyAndDeleteWithoutPrompt(t *testing.T) {
	source := stubBinary(t)
	mockCurrentBinary(t, source)
	t.Setenv("PATH", t.TempDir())
	req := Request{Mode: config.ModeGlobal}
	applyGlobalInstallDefaults(&req)
	if !req.CopyBinary || !req.DeleteLegacy {
		t.Fatalf("copy=%v delete=%v", req.CopyBinary, req.DeleteLegacy)
	}

	t.Setenv("PATH", filepath.Dir(source))
	req = Request{Mode: config.ModeGlobal}
	applyGlobalInstallDefaults(&req)
	if req.CopyBinary || !req.DeleteLegacy {
		t.Fatalf("matched path still copied or skipped legacy delete: copy=%v delete=%v", req.CopyBinary, req.DeleteLegacy)
	}
}

func TestCopyForStartupMatchingPathDoesNotPrompt(t *testing.T) {
	source := stubBinary(t)
	mockCurrentBinary(t, source)
	t.Setenv("PATH", filepath.Dir(source))
	previous := confirmCopy
	confirmCopy = func(pathCheck, string) (bool, error) {
		t.Fatal("unexpected copy prompt")
		return false, nil
	}
	t.Cleanup(func() { confirmCopy = previous })
	handled, code := copyForStartup(config.InstallPaths{BinDir: t.TempDir()})
	if handled || code != 0 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
}

func TestPathConfirmationExplainsReplacementAndUnknown(t *testing.T) {
	setupInstallHome(t)
	check := inspectPath(filepath.Join(t.TempDir(), "missing"))
	dest := filepath.Join(t.TempDir(), binaryName())
	description := copyDescription(check, dest)
	if !strings.Contains(description, check.Current) || !strings.Contains(description, dest) || !strings.Contains(description, check.Err.Error()) {
		t.Fatalf("missing paths or diagnostic: %s", description)
	}
}

func TestWindowsPathDoesNotSearchImplicitWorkingDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command lookup")
	}
	source := stubBinary(t)
	t.Chdir(filepath.Dir(source))
	t.Setenv("PATH", t.TempDir())
	if check := inspectPath(source); check.Found != "" || check.Matches {
		t.Fatalf("implicit current directory counted as PATH: %+v", check)
	}
	t.Setenv("PATHEXT", ".CMD;.EXE")
	t.Setenv("PATH", filepath.Dir(source))
	shim := filepath.Join(filepath.Dir(source), "kander.cmd")
	if err := os.WriteFile(shim, []byte("@echo shim"), 0o644); err != nil {
		t.Fatal(err)
	}
	if check := inspectPath(source); check.Found != shim || check.Matches {
		t.Fatalf("PATHEXT priority ignored: %+v", check)
	}
}
