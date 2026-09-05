package review

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

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

func gitRepo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, repo, name, body, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, repo, "add", name)
	gitRepo(t, repo, "commit", "-q", "-m", message)
	return gitRepo(t, repo, "rev-parse", "HEAD")
}

func setupLang(t *testing.T, configPath string) {
	t.Helper()
	t.Setenv(config.EnvConfig, configPath)
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func assertArg(t *testing.T, argv []string, flag, value string) {
	t.Helper()
	for i, v := range argv {
		if v == flag {
			if i+1 >= len(argv) || argv[i+1] != value {
				t.Fatalf("%s want %q in %v", flag, value, argv)
			}
			return
		}
	}
	t.Fatalf("missing %s in %v", flag, argv)
}
