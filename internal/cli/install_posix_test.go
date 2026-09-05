//go:build unix

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
	posixRepoRoot string
	posixKander   string
)

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		os.Exit(1)
	}
	posixRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	dir, err := os.MkdirTemp("", "kander-cli-bin")
	if err != nil {
		os.Exit(1)
	}
	posixKander = filepath.Join(dir, "kander")
	cmd := exec.Command("go", "build", "-o", posixKander, "./cmd/kander")
	cmd.Dir = posixRepoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func copyFile(t *testing.T, src, dest string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
		mode := info.Mode()
		if mode&0o111 != 0 {
			copyFile(t, path, filepath.Join(dest, rel), 0o755)
		} else {
			copyFile(t, path, filepath.Join(dest, rel), 0o644)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeStubKander(t *testing.T, dest, script string) {
	t.Helper()
	if err := os.WriteFile(dest, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func installerSource(t *testing.T, binary string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "src")
	copyFile(t, filepath.Join(posixRepoRoot, "install.sh"), filepath.Join(dest, "install.sh"), 0o755)
	copyDirFiles(t, filepath.Join(posixRepoRoot, "rules"), filepath.Join(dest, "rules"))
	copyDirFiles(t, filepath.Join(posixRepoRoot, "share"), filepath.Join(dest, "share"))
	if binary == "" {
		copyFile(t, posixKander, filepath.Join(dest, "kander"), 0o755)
	} else {
		copyFile(t, binary, filepath.Join(dest, "kander"), 0o755)
	}
	return dest
}

func installEnv(home string, extra map[string]string) []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	skip := map[string]struct{}{
		"HOME": {}, "KANDER_LANG": {}, "KANDER_LANG_CLI": {},
		"LC_ALL": {}, "LC_MESSAGES": {}, "LANG": {},
		"GIT_DIR": {}, "GIT_WORK_TREE": {}, "GIT_COMMON_DIR": {},
	}
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		if _, ok := skip[name]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = append(filtered, "HOME="+home, "KANDER_LANG=cn")
	for key, value := range extra {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func runInstaller(t *testing.T, src, home string, stdin string, extra map[string]string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{filepath.Join(src, "install.sh")}, args...)...)
	cmd.Dir = src
	cmd.Env = installEnv(home, extra)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	} else {
		cmd.Stdin = bytes.NewReader(nil)
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

func initGitRepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "init", "-q", path)
	runGit(t, path, "-C", path, "config", "user.email", "kander@example.com")
	runGit(t, path, "-C", path, "config", "user.name", "Kander Test")
	runGit(t, path, "-C", path, "commit", "--allow-empty", "-q", "-m", "init")
	return path
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func assertFileEqual(t *testing.T, src, dest string) {
	t.Helper()
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s content mismatch", dest)
	}
}

func TestInstallerGlobalCopiesPayload(t *testing.T) {
	src := installerSource(t, "")
	home := filepath.Join(t.TempDir(), "home")
	legacy := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"onevoke", "kanban", "onevoke-review.sh"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte("legacy\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	code, out, err := runInstaller(t, src, home, "", nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if out != "Kander 已安装\n" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "检测到已退役的 onevoke/kanban 入口") {
		t.Fatalf("stderr=%s", err)
	}
	if !strings.Contains(err, "已保留旧入口") {
		t.Fatalf("stderr=%s", err)
	}
	installed := filepath.Join(home, ".local", "bin", "kander")
	assertFileEqual(t, filepath.Join(src, "kander"), installed)
	st, statErr := os.Stat(installed)
	if statErr != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("kander not executable: %v", statErr)
	}
	rules, walkErr := filepath.Glob(filepath.Join(src, "rules", "*.md"))
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	for _, rule := range rules {
		assertFileEqual(t, rule, filepath.Join(home, ".agents", filepath.Base(rule)))
	}
	agents := filepath.Join(home, ".agents", "AGENTS.md")
	link, readErr := os.Readlink(agents)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if link != "KANDER-AGENTS.md" {
		t.Fatalf("link=%s", link)
	}
	for _, name := range []string{"onevoke", "kanban", "onevoke-review.sh"} {
		got, _ := os.ReadFile(filepath.Join(legacy, name))
		if string(got) != "legacy\n" {
			t.Fatalf("legacy %s mutated", name)
		}
	}
	help := exec.Command(installed, "--help")
	help.Env = installEnv(home, nil)
	helpOut, helpErr := help.CombinedOutput()
	if help.ProcessState.ExitCode() != 0 {
		t.Fatalf("installed --help: %s", helpOut)
	}
	if !strings.Contains(string(helpOut), "list / ls") {
		t.Fatalf("help=%s err=%v", helpOut, helpErr)
	}
}

func TestInstallerRemovesLegacyAfterConfirm(t *testing.T) {
	src := installerSource(t, "")
	home := filepath.Join(t.TempDir(), "home")
	legacy := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"onevoke", "kanban"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte("legacy\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	code, out, err := runInstaller(t, src, home, "y\n", nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if out != "Kander 已安装\n" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "已删除旧入口") {
		t.Fatalf("stderr=%s", err)
	}
	for _, name := range []string{"onevoke", "kanban"} {
		if _, statErr := os.Stat(filepath.Join(legacy, name)); !os.IsNotExist(statErr) {
			t.Fatalf("%s still present", name)
		}
	}
}

func TestInstallerPreservesExistingAgentsEntry(t *testing.T) {
	src := installerSource(t, "")
	home := filepath.Join(t.TempDir(), "home")
	agents := filepath.Join(home, ".agents", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agents), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agents, []byte("本机规则\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, err := runInstaller(t, src, home, "", nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	got, _ := os.ReadFile(agents)
	if string(got) != "本机规则\n" {
		t.Fatalf("got %q", got)
	}
}

func TestInstallerRejectsDirectoryFileTarget(t *testing.T) {
	src := installerSource(t, "")
	home := filepath.Join(t.TempDir(), "home")
	blocked := filepath.Join(home, ".agents", "KANDER-AGENTS.md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, err := runInstaller(t, src, home, "", nil)
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if out != "" {
		t.Fatalf("stdout=%q", out)
	}
	if !strings.Contains(err, "安装目标是目录") {
		t.Fatalf("stderr=%s", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "bin", "kander")); !os.IsNotExist(statErr) {
		t.Fatal("wrote kander after reject")
	}
}

func TestInstallerRejectsLegacyDirectory(t *testing.T) {
	src := installerSource(t, "")
	home := filepath.Join(t.TempDir(), "home")
	blocked := filepath.Join(home, ".local", "bin", "onevoke")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, err := runInstaller(t, src, home, "", nil)
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if !strings.Contains(err, "旧版安装目标是目录") {
		t.Fatalf("stderr=%s", err)
	}
}

func TestInstallerArguments(t *testing.T) {
	src := installerSource(t, "")
	home := filepath.Join(t.TempDir(), "home")
	code, out, _ := runInstaller(t, src, home, "", map[string]string{"KANDER_LANG": "en"}, "--help")
	if code != 0 || !strings.Contains(out, "usage: install.sh") {
		t.Fatalf("help code=%d out=%s", code, out)
	}
	code, _, err := runInstaller(t, src, home, "", map[string]string{"KANDER_LANG": "en"}, "--lang=fr")
	if code != 2 || !strings.Contains(err, "--lang must be cn or en") {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(statErr) {
		t.Fatal("invalid lang wrote files")
	}
}

func TestInstallerRequiresBinary(t *testing.T) {
	srcDir := t.TempDir()
	copyFile(t, filepath.Join(posixRepoRoot, "install.sh"), filepath.Join(srcDir, "install.sh"), 0o755)
	if err := os.Mkdir(filepath.Join(srcDir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rules", "KANDER-AGENTS.md"), []byte("# kander\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	code, _, err := runInstaller(t, srcDir, home, "", map[string]string{"PATH": "/usr/bin:/bin"})
	if code == 0 {
		t.Fatal("expected failure without binary or go module")
	}
	if !strings.Contains(err, "go build 失败") && !strings.Contains(err, "go 不可用") && !strings.Contains(err, "go build failed") {
		t.Fatalf("stderr=%s", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "bin", "kander")); !os.IsNotExist(statErr) {
		t.Fatal("installed despite missing binary")
	}
}

func TestProjectInstallCopiesPayloadAndSkipsGlobal(t *testing.T) {
	src := installerSource(t, "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(home, "keep")
	if err := os.WriteFile(canary, []byte("home-canary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := initGitRepo(t, filepath.Join(root, "app"))
	code, out, err := runInstaller(t, src, home, "", nil, "--project", project)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "Kander 已安装" {
		t.Fatalf("stdout=%q", out)
	}
	destBin := filepath.Join(project, ".kander", "bin", "kander")
	if lines[1] != destBin {
		t.Fatalf("path=%q want %q", lines[1], destBin)
	}
	if !strings.Contains(err, "未修改 PATH") {
		t.Fatalf("stderr=%s", err)
	}
	assertFileEqual(t, filepath.Join(src, "kander"), destBin)
	assertFileEqual(t, filepath.Join(src, "rules", "KANDER-AGENTS.md"), filepath.Join(project, ".kander", "rules", "KANDER-AGENTS.md"))
	for _, module := range []string{"COLLABORATION", "CODE", "GIT", "REVIEW", "TASK-INTAKE", "TASK-GROUP", "REPORTING"} {
		name := "KANDER-" + module + "-RULES.md"
		assertFileEqual(t, filepath.Join(src, "rules", name), filepath.Join(project, ".kander", "rules", name))
	}
	exclude, _ := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
	if !strings.Contains(string(exclude), "/.kander/") {
		t.Fatalf("exclude=%q", exclude)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(statErr) {
		t.Fatal("created HOME install paths")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(statErr) {
		t.Fatal("created HOME agents")
	}
	got, _ := os.ReadFile(canary)
	if string(got) != "home-canary\n" {
		t.Fatal("home mutated")
	}
}

func TestProjectInstallFromLinkedWorktree(t *testing.T) {
	src := installerSource(t, "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	main := initGitRepo(t, filepath.Join(root, "app"))
	linked := filepath.Join(root, "app-linked")
	runGit(t, main, "-C", main, "worktree", "add", "-q", linked, "HEAD")
	code, out, err := runInstaller(t, src, home, "", nil, "--lang", "cn", "--project", linked)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if !strings.Contains(out, filepath.Join(main, ".kander", "bin", "kander")) {
		t.Fatalf("stdout=%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(linked, ".kander")); !os.IsNotExist(statErr) {
		t.Fatal("installed into linked worktree")
	}
}

func TestProjectInstallRejectsNonGit(t *testing.T) {
	src := installerSource(t, "")
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
	if !strings.Contains(err, "项目不是 Git 仓库") {
		t.Fatalf("stderr=%s", err)
	}
	if strings.Contains(out, "Kander 已安装") {
		t.Fatalf("stdout=%q", out)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(statErr) {
		t.Fatal("fell back to global")
	}
}

func TestProjectInstallRejectsMissingAndSymlink(t *testing.T) {
	src := installerSource(t, "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	code, _, err := runInstaller(t, src, home, "", nil, "--project", filepath.Join(root, "missing"))
	if code == 0 || !strings.Contains(err, "项目目录不存在") {
		t.Fatalf("missing: code=%d stderr=%s", code, err)
	}
	project := initGitRepo(t, filepath.Join(root, "app"))
	link := filepath.Join(root, "app-link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	code, _, err = runInstaller(t, src, home, "", nil, "--project", link)
	if code == 0 {
		t.Fatal("expected symlink reject")
	}
	if !strings.Contains(err, "符号链接") && !strings.Contains(err, "symlink") {
		t.Fatalf("stderr=%s", err)
	}
}

func TestProjectInstallRejectsKanderSymlink(t *testing.T) {
	src := installerSource(t, "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := initGitRepo(t, filepath.Join(root, "app"))
	outside := filepath.Join(root, "payload")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, ".kander")); err != nil {
		t.Fatal(err)
	}
	code, _, err := runInstaller(t, src, home, "", nil, "--project", project)
	if code == 0 || !strings.Contains(err, "符号链接") {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	got, _ := os.ReadFile(filepath.Join(outside, "keep"))
	if string(got) != "outside\n" {
		t.Fatal("followed symlink")
	}
}

func TestProjectInstallRejectsDirectoryTarget(t *testing.T) {
	src := installerSource(t, "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := initGitRepo(t, filepath.Join(root, "app"))
	blocked := filepath.Join(project, ".kander", "bin", "kander")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, err := runInstaller(t, src, home, "", nil, "--project", project)
	if code != 1 || !strings.Contains(err, "安装目标是目录") {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	exclude, _ := os.ReadFile(filepath.Join(project, ".git", "info", "exclude"))
	if strings.Contains(string(exclude), "/.kander/") {
		t.Fatalf("exclude written before reject: %s", exclude)
	}
}

func TestProjectInstallRejectsInvalidArguments(t *testing.T) {
	src := installerSource(t, "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := initGitRepo(t, filepath.Join(root, "app"))
	code, _, err := runInstaller(t, src, home, "", nil, "--project")
	if code != 2 || !strings.Contains(err, "--project 需要目录") {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	code, _, err = runInstaller(t, src, home, "", nil, "--project", project, "--force")
	if code != 2 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".kander")); !os.IsNotExist(statErr) {
		t.Fatal("wrote project install on bad args")
	}
	code, _, err = runInstaller(t, src, home, "", nil, "--project", project, "--project", project)
	if code != 2 || !strings.Contains(err, "--project 只能指定一次") {
		t.Fatalf("dup code=%d stderr=%s", code, err)
	}
}

func TestInstallerSkipsNonFileRules(t *testing.T) {
	srcDir := t.TempDir()
	copyFile(t, filepath.Join(posixRepoRoot, "install.sh"), filepath.Join(srcDir, "install.sh"), 0o755)
	copyFile(t, posixKander, filepath.Join(srcDir, "kander"), 0o755)
	if err := os.MkdirAll(filepath.Join(srcDir, "rules", "ignored.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rules", "REAL.md"), []byte("# real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	code, _, err := runInstaller(t, srcDir, home, "", nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents", "REAL.md")); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents", "ignored.md")); !os.IsNotExist(statErr) {
		t.Fatal("copied directory rule")
	}
}
