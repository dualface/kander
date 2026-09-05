package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/version"
)

func resetLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
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

func TestHelpListsAllCommandsAndLang(t *testing.T) {
	resetLang(t)
	code, out, err := captureRun(t, []string{"kander", "--help"})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	required := []string{
		"doctor", "config", "review", "init",
		"version",
		"list / ls", "show", "new", "move", "pick", "start", "resume",
		"notify", "dismiss", "check", "subscribe",
		"--lang {cn,en}",
	}
	for _, item := range required {
		if !strings.Contains(out, item) {
			t.Fatalf("help missing %q\n%s", item, out)
		}
	}
}

func TestHelpEnglish(t *testing.T) {
	resetLang(t)
	code, out, err := captureRun(t, []string{"kander", "--lang", "en", "--help"})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(out, "usage: kander") {
		t.Fatalf("expected english help: %s", out)
	}
}

func TestUnimplementedCommands(t *testing.T) {
	resetLang(t)
	implemented := map[string]struct{}{
		"init": {}, "list": {}, "ls": {}, "show": {},
		"new": {}, "move": {}, "pick": {}, "check": {},
		"guard-write": {},
		"doctor":      {}, "config": {},
		"version": {},
	}
	names := append([]string{"ls"}, commandNames...)
	for _, name := range names {
		if _, ok := implemented[name]; ok {
			continue
		}
		code, _, err := captureRun(t, []string{"kander", name})
		if code == 0 {
			t.Fatalf("%s exited 0", name)
		}
		canonical := name
		if name == "ls" {
			canonical = "list"
		}
		want := "子命令尚未实现: " + canonical
		if !strings.Contains(err, want) {
			t.Fatalf("%s stderr=%q want %q", name, err, want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	resetLang(t)
	oldTimestamp, oldHash := version.BuildTimestamp, version.GitHash
	t.Cleanup(func() {
		version.BuildTimestamp, version.GitHash = oldTimestamp, oldHash
	})
	version.BuildTimestamp = "20260906T123456Z"
	version.GitHash = "0123456789ab"
	code, out, err := captureRun(t, []string{"kander", "version"})
	if code != 0 || out != "kander 20260906T123456Z-0123456789ab\n" || err != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, err)
	}
	code, out, err = captureRun(t, []string{"kander", "version", "--help"})
	if code != 0 || !strings.Contains(out, "kander version") || err != "" {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, out, err)
	}
	code, _, err = captureRun(t, []string{"kander", "version", "extra"})
	if code != 2 || !strings.Contains(err, "kander version") {
		t.Fatalf("code=%d stderr=%q", code, err)
	}
}

func TestUnimplementedEnglish(t *testing.T) {
	resetLang(t)
	code, _, err := captureRun(t, []string{"kander", "--lang=en", "start"})
	if code == 0 {
		t.Fatal("expected non-zero")
	}
	if !strings.Contains(err, "subcommand is not implemented: start") {
		t.Fatalf("stderr=%q", err)
	}
}

func TestInvalidLang(t *testing.T) {
	resetLang(t)
	code, _, err := captureRun(t, []string{"kander", "--lang", "fr"})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(err, "--lang 只接受 cn 或 en") && !strings.Contains(err, "--lang must be cn or en") {
		t.Fatalf("stderr=%q", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	resetLang(t)
	for _, name := range []string{"nope", "rules", "tui", "welcome"} {
		code, _, err := captureRun(t, []string{"kander", name})
		if code != 2 {
			t.Fatalf("%s code=%d", name, code)
		}
		if !strings.Contains(err, "未知命令 "+name) {
			t.Fatalf("%s stderr=%q", name, err)
		}
	}
}

func TestDefaultRunner(t *testing.T) {
	resetLang(t)
	old := DefaultRunner
	t.Cleanup(func() { DefaultRunner = old })
	called := false
	DefaultRunner = func(args []string) int {
		called = true
		if len(args) != 0 {
			t.Fatalf("args=%v", args)
		}
		return 7
	}
	if code := Run([]string{"kander"}); code != 7 || !called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}

// Windows PowerShell 5.1 用 ANSI 代码页读没有 BOM 的 .ps1, 会把安装器里的中文
// 拆成乱码并直接语法报错. install.ps1 必须保留 UTF-8 BOM.
func TestInstallerScriptKeepsUTF8BOM(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	script := filepath.Join(filepath.Dir(file), "..", "..", "install.ps1")
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("install.ps1 lost its UTF-8 BOM: % x", data[:min(3, len(data))])
	}
}
