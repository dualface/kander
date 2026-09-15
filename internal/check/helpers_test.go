package check

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func setupCheckLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
}

func captureRun(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW
	code := Run(args)
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	errb, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatal(err)
	}
	_ = stdoutR.Close()
	_ = stderrR.Close()
	return code, string(out), string(errb)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "commit", "--allow-empty", "-q", "-m", "root")
	return dir
}

func copyCommit(t *testing.T, dir, src, dst, message string) string {
	t.Helper()
	in, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src)))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, filepath.FromSlash(dst))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, in, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "--", dst)
	git(t, dir, "commit", "-q", "-m", message)
	return git(t, dir, "rev-parse", "HEAD")
}

func writeCommit(t *testing.T, dir, rel, body, message string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "--", rel)
	git(t, dir, "commit", "-q", "-m", message)
	return git(t, dir, "rev-parse", "HEAD")
}

func nLines(n int, trailingNL bool) string {
	var b strings.Builder
	for range n {
		b.WriteString("line-")
		b.WriteString(strings.Repeat("x", 8))
		b.WriteByte('\n')
	}
	s := b.String()
	if !trailingNL {
		s = strings.TrimSuffix(s, "\n")
	}
	return s
}
