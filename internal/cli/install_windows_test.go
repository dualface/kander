//go:build windows

package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	winRepoRoot string
	winKander   string
	powershell  string
)

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		os.Exit(1)
	}
	winRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	powershell, _ = exec.LookPath("pwsh.exe")
	if powershell == "" {
		powershell, _ = exec.LookPath("powershell.exe")
	}
	dir, err := os.MkdirTemp("", "kander-cli-bin")
	if err != nil {
		os.Exit(1)
	}
	winKander = filepath.Join(dir, "kander.exe")
	cmd := exec.Command("go", "build", "-o", winKander, "./cmd/kander")
	cmd.Dir = winRepoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func copyFile(t *testing.T, src, dest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func copyDirFiles(t *testing.T, src, dest string) {
	t.Helper()
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return
	}
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		copyFile(t, path, filepath.Join(dest, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func installerSource(t *testing.T) string {
	t.Helper()
	return installerSourceWithBinary(t, winKander)
}

func installerSourceWithBinary(t *testing.T, binary string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "src")
	copyFile(t, filepath.Join(winRepoRoot, "install.ps1"), filepath.Join(dest, "install.ps1"))
	copyDirFiles(t, filepath.Join(winRepoRoot, "rules"), filepath.Join(dest, "rules"))
	copyDirFiles(t, filepath.Join(winRepoRoot, "share"), filepath.Join(dest, "share"))
	if binary != "" {
		copyFile(t, binary, filepath.Join(dest, "kander.exe"))
	}
	return dest
}

func initGitRepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", path)
	run("-C", path, "config", "user.email", "kander@example.com")
	run("-C", path, "config", "user.name", "Kander Test")
	run("-C", path, "commit", "--allow-empty", "-q", "-m", "init")
	return path
}

func assertSameFile(t *testing.T, left, right string) {
	t.Helper()
	a, err := os.Stat(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(right)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatalf("%s is not the same file as %s", left, right)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected missing %s: %v", path, err)
	}
}

func installEnv(home string, extra map[string]string) []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	skip := map[string]struct{}{
		"HOME": {}, "USERPROFILE": {}, "KANDER_LANG": {},
		"LC_ALL": {}, "LC_MESSAGES": {}, "LANG": {},
	}
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		if _, ok := skip[strings.ToUpper(name)]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = append(filtered, "HOME="+home, "USERPROFILE="+home, "KANDER_LANG=cn")
	for key, value := range extra {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func runInstaller(t *testing.T, src, home, stdin string, extra map[string]string, args ...string) (int, string, string) {
	t.Helper()
	if powershell == "" {
		t.Skip("PowerShell is required")
	}
	command := []string{
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", filepath.Join(src, "install.ps1"),
	}
	command = append(command, args...)
	cmd := exec.Command(powershell, command...)
	cmd.Dir = src
	cmd.Env = installEnv(home, extra)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run installer: %v\n%s", err, stderr.String())
		}
	}
	return code, stdout.String(), stderr.String()
}

func TestWindowsInstallerCopiesPayloadAndHintsPath(t *testing.T) {
	src := installerSource(t)
	home := filepath.Join(t.TempDir(), "home")
	legacy := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "onevoke.cmd"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "kanban.cmd"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, err := runInstaller(t, src, home, "", nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if out != "Kander 已安装\n" && out != "Kander 已安装\r\n" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "安装器不会自动修改用户 PATH") {
		t.Fatalf("stderr=%s", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "bin", "kander.exe")); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents", "KANDER-AGENTS.md")); statErr != nil {
		t.Fatal(statErr)
	}
	assertSameFile(t, filepath.Join(home, ".agents", "AGENTS.md"), filepath.Join(home, ".agents", "KANDER-AGENTS.md"))
	got, _ := os.ReadFile(filepath.Join(legacy, "onevoke.cmd"))
	if string(got) != "legacy\n" {
		t.Fatal("legacy removed without confirm")
	}
}

func TestWindowsInstallerUsesUserProfile(t *testing.T) {
	src := installerSource(t)
	profile := filepath.Join(t.TempDir(), "windows-profile")
	unrelated := filepath.Join(t.TempDir(), "git-bash-home")
	code, _, err := runInstaller(t, src, profile, "", map[string]string{"HOME": unrelated})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if _, statErr := os.Stat(filepath.Join(profile, ".local", "bin", "kander.exe")); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(filepath.Join(unrelated, ".local")); !os.IsNotExist(statErr) {
		t.Fatal("used HOME instead of USERPROFILE")
	}
}

func TestWindowsInstallerRejectsDirectoryTarget(t *testing.T) {
	src := installerSource(t)
	home := filepath.Join(t.TempDir(), "home")
	blocked := filepath.Join(home, ".agents", "KANDER-AGENTS.md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, err := runInstaller(t, src, home, "", nil)
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "安装目标是目录") {
		t.Fatalf("stderr=%s", err)
	}
	assertNotExist(t, filepath.Join(home, ".local", "bin"))
	assertNotExist(t, filepath.Join(home, ".agents"))
}

func TestWindowsInstallerLanguageValidation(t *testing.T) {
	src := installerSource(t)
	home := filepath.Join(t.TempDir(), "home")
	code, out, _ := runInstaller(t, src, home, "", map[string]string{"KANDER_LANG": "en"}, "--help")
	if code != 0 || !strings.Contains(out, "usage: install.ps1") {
		t.Fatalf("help code=%d out=%s", code, out)
	}
	code, _, err := runInstaller(t, src, home, "", map[string]string{"KANDER_LANG": "en"}, "--lang=fr")
	if code != 2 || !strings.Contains(err, "--lang must be cn or en") {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	assertNotExist(t, filepath.Join(home, ".local"))
	assertNotExist(t, filepath.Join(home, ".agents"))
}

func TestWindowsProjectInstallAvoidsHome(t *testing.T) {
	src := installerSource(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	project := initGitRepo(t, filepath.Join(root, "app"))
	code, out, err := runInstaller(t, src, home, "", nil, "--project", project)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if !strings.Contains(out, "Kander 已安装") {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(out, `.kander\bin\kander.exe`) && !strings.Contains(out, `.kander/bin/kander.exe`) {
		t.Fatalf("missing project path: %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(statErr) {
		t.Fatal("wrote HOME")
	}
	destBin := filepath.Join(project, ".kander", "bin", "kander.exe")
	if _, err := os.Stat(destBin); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".kander", "rules", "KANDER-AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	exclude, readErr := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(exclude), "/.kander/") {
		t.Fatalf("exclude=%q", exclude)
	}
}

func TestWindowsProjectInstallRejectsNonGit(t *testing.T) {
	src := installerSource(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	directory := filepath.Join(root, "not-git")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, err := runInstaller(t, src, home, "", nil, "--project", directory)
	if code == 0 {
		t.Fatal("expected failure")
	}
	if strings.Contains(out, "Kander 已安装") {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "Git") && !strings.Contains(err, "git") {
		t.Fatalf("stderr=%s", err)
	}
}

func TestWindowsInstallerRemovesLegacyAfterConfirm(t *testing.T) {
	src := installerSource(t)
	home := filepath.Join(t.TempDir(), "home")
	legacy := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "onevoke.cmd"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "kanban.cmd"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, err := runInstaller(t, src, home, "y\n", nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if out != "Kander 已安装\n" && out != "Kander 已安装\r\n" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "已删除旧入口") {
		t.Fatalf("stderr=%s", err)
	}
	assertNotExist(t, filepath.Join(legacy, "onevoke.cmd"))
	assertNotExist(t, filepath.Join(legacy, "kanban.cmd"))
}

func TestWindowsInstallerPreservesExistingAgentsEntry(t *testing.T) {
	src := installerSource(t)
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home-"+kind)
			agents := filepath.Join(home, ".agents", "AGENTS.md")
			if err := os.MkdirAll(filepath.Dir(agents), 0o755); err != nil {
				t.Fatal(err)
			}
			if kind == "file" {
				if err := os.WriteFile(agents, []byte("本机规则\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(agents, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(agents, "keep"), []byte("keep\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			code, _, err := runInstaller(t, src, home, "", nil)
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, err)
			}
			if kind == "file" {
				got, readErr := os.ReadFile(agents)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(got) != "本机规则\n" {
					t.Fatalf("got %q", got)
				}
			} else {
				got, readErr := os.ReadFile(filepath.Join(agents, "keep"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(got) != "keep\n" {
					t.Fatalf("got %q", got)
				}
			}
		})
	}
}

func TestWindowsInstallerRequiresBinary(t *testing.T) {
	src := installerSourceWithBinary(t, "")
	home := filepath.Join(t.TempDir(), "home")
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	isolated := filepath.Join(t.TempDir(), "empty-bin")
	if err := os.Mkdir(isolated, 0o755); err != nil {
		t.Fatal(err)
	}
	path := isolated + string(os.PathListSeparator) + filepath.Join(systemRoot, "System32")
	code, _, err := runInstaller(t, src, home, "", map[string]string{"PATH": path})
	if code == 0 {
		t.Fatal("expected failure without binary or go")
	}
	if !strings.Contains(err, "go") && !strings.Contains(err, "kander") {
		t.Fatalf("stderr=%s", err)
	}
	assertNotExist(t, filepath.Join(home, ".local", "bin", "kander.exe"))
	assertNotExist(t, filepath.Join(home, ".agents"))
}

func TestWindowsInstallerRejectsAgentsDirIsFile(t *testing.T) {
	src := installerSource(t)
	home := filepath.Join(t.TempDir(), "home")
	blocked := filepath.Join(home, ".agents")
	if err := os.MkdirAll(filepath.Dir(blocked), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, err := runInstaller(t, src, home, "", nil)
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "安装目标不是目录") && !strings.Contains(err, "installation target is not a directory") {
		t.Fatalf("stderr=%s", err)
	}
	assertNotExist(t, filepath.Join(home, ".local", "bin"))
	assertNotExist(t, filepath.Join(home, ".agents"))
}
