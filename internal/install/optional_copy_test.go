package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

func mockCurrentBinary(t *testing.T, path string) {
	t.Helper()
	previous := lookupExecutable
	lookupExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { lookupExecutable = previous })
}

func TestGlobalInitializationDoesNotCopy(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "old-copy"}[existing], func(t *testing.T) {
			home := setupInstallHome(t)
			source := stubBinary(t)
			mockCurrentBinary(t, source)
			dest := filepath.Join(home, ".local", "bin", binaryName())
			if existing {
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, []byte("old binary"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			result, err := Perform(Request{Language: "en"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Copied || result.DestBinary != "" || !sameFile(result.RunBinary, source) {
				t.Fatalf("unexpected binary install: %+v", result)
			}
			if existing {
				got, err := os.ReadFile(dest)
				if err != nil || string(got) != "old binary" {
					t.Fatalf("old binary changed: %q %v", got, err)
				}
			} else if _, err := os.Lstat(filepath.Dir(dest)); !os.IsNotExist(err) {
				t.Fatalf("created binary directory: %v", err)
			}
			for _, name := range rules.Names() {
				if _, err := os.Stat(filepath.Join(result.Paths.RulesDir, name)); err != nil {
					t.Fatal(err)
				}
			}
			previous := handoff
			calls := 0
			handoff = func(path string, argv, env []string) error {
				calls++
				if !sameFile(path, source) {
					t.Fatalf("handoff selected a stale copy: %s", path)
				}
				assertHandoffInvocation(t, path, "en", argv, env)
				return nil
			}
			t.Cleanup(func() { handoff = previous })
			if code := finishSuccessfulInstall(result, "en"); code != 0 || calls != 1 {
				t.Fatalf("handoff code=%d calls=%d", code, calls)
			}
		})
	}
}

func TestProjectInitializationStillCopies(t *testing.T) {
	setupInstallHome(t)
	source := stubBinary(t)
	project := initGitRepo(t, t.TempDir())
	result, err := Perform(Request{Mode: config.ModeProject, Project: project, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Copied || result.RunBinary != result.DestBinary || result.DestBinary == source {
		t.Fatalf("project did not install its own binary: %+v", result)
	}
}

func TestStartupCopyIsOptionalAndBinaryOnly(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "accept"}[accept], func(t *testing.T) {
			setupInstallHome(t)
			source := stubBinary(t)
			mockCurrentBinary(t, source)
			paths, err := config.GlobalInstallPaths()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := config.Save(config.DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(paths.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(paths.BinDir, binaryName())
			if err := os.WriteFile(dest, []byte("old copy"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", paths.BinDir)
			previousConfirm, previousHandoff := confirmCopy, handoff
			t.Cleanup(func() { confirmCopy, handoff = previousConfirm, previousHandoff })
			prompts, starts := 0, 0
			confirmCopy = func(check pathCheck, target string) (bool, error) {
				prompts++
				if check.Matches || check.Found != dest || target != dest {
					t.Fatalf("incorrect confirmation: %+v %s", check, target)
				}
				return accept, nil
			}
			handoff = func(path string, argv, env []string) error {
				starts++
				if path != dest {
					t.Fatalf("handoff=%s", path)
				}
				assertHandoffInvocation(t, path, config.ResolveLanguage(), argv, env)
				return nil
			}
			if handled, code := copyForStartup(paths); handled != accept || code != 0 {
				t.Fatalf("handled=%v code=%d", handled, code)
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if accept {
				want, _ := os.ReadFile(source)
				if !bytes.Equal(got, want) || starts != 1 {
					t.Fatalf("copy=%q starts=%d", got, starts)
				}
			} else {
				if string(got) != "old copy" || starts != 0 {
					t.Fatalf("declining changed the binary: %q", got)
				}
				copyForStartup(paths)
				if prompts != 2 {
					t.Fatalf("declining persisted a skip: prompts=%d", prompts)
				}
			}
			after, err := os.ReadFile(paths.ConfigPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("binary-only install rewrote config")
			}
			if _, err := os.Lstat(paths.RulesDir); !os.IsNotExist(err) {
				t.Fatalf("binary-only install wrote rules: %v", err)
			}
		})
	}
}

func TestStartupCopyFailureDoesNotHandoff(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "destination-directory", true: "cancel"}[cancel], func(t *testing.T) {
			setupInstallHome(t)
			mockCurrentBinary(t, stubBinary(t))
			t.Setenv("PATH", t.TempDir())
			paths, err := config.GlobalInstallPaths()
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(paths.BinDir, binaryName())
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			previousConfirm, previousHandoff := confirmCopy, handoff
			t.Cleanup(func() { confirmCopy, handoff = previousConfirm, previousHandoff })
			confirmCopy = func(pathCheck, string) (bool, error) {
				if cancel {
					return false, errors.New("cancelled")
				}
				return true, nil
			}
			handoff = func(string, []string, []string) error { t.Fatal("handoff after failure"); return nil }
			if handled, code := copyForStartup(paths); !handled || code != 1 {
				t.Fatalf("handled=%v code=%d", handled, code)
			}
			info, err := os.Stat(dest)
			if err != nil || !info.IsDir() {
				t.Fatalf("destination changed: %v", err)
			}
		})
	}
}

func TestGlobalNoCopyIgnoresBinaryDestinationDirectory(t *testing.T) {
	home := setupInstallHome(t)
	mockCurrentBinary(t, stubBinary(t))
	dest := filepath.Join(home, ".local", "bin", binaryName())
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(Request{Language: "en"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil || !info.IsDir() {
		t.Fatalf("destination changed: %v", err)
	}
}
