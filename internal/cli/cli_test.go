package cli

import (
	"io"
	"os"
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
		"doctor", "config", "init",
		"version",
		"install",
		"issue",
		"show", "new", "move", "pick", "request",
		"--lang {cn,en,ja}",
	}
	for _, item := range required {
		if !strings.Contains(out, item) {
			t.Fatalf("help missing %q\n%s", item, out)
		}
	}
	if strings.Contains(out, "worker") {
		t.Fatalf("help lists the internal worker command\n%s", out)
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

func TestVersionCommand(t *testing.T) {
	resetLang(t)
	old := version.Version
	t.Cleanup(func() {
		version.Version = old
	})
	version.Version = "0.5.0"
	code, out, err := captureRun(t, []string{"kander", "version"})
	if code != 0 || out != "kander 0.5.0\n" || err != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, err)
	}
	code, out, err = captureRun(t, []string{"kander", "version", "--help"})
	if code != 0 || !strings.Contains(out, "kander version") || !onlyFallbackHint(err) {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, out, err)
	}
	code, _, err = captureRun(t, []string{"kander", "version", "extra"})
	if code != 2 || !strings.Contains(err, "kander version") {
		t.Fatalf("code=%d stderr=%q", code, err)
	}
}

func TestInvalidLang(t *testing.T) {
	resetLang(t)
	code, _, err := captureRun(t, []string{"kander", "--lang", "fr"})
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(err, "--lang 只接受 cn、en 或 ja") && !strings.Contains(err, "--lang must be cn, en, or ja") {
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

func TestInstallRejectsNonInteractiveFlags(t *testing.T) {
	resetLang(t)
	for _, arg := range []string{"--yes", "--scope", "--project"} {
		code, _, err := captureRun(t, []string{"kander", "install", arg})
		if code != 2 {
			t.Fatalf("%s code=%d stderr=%q", arg, code, err)
		}
		if !strings.Contains(err, arg) {
			t.Fatalf("%s stderr=%q", arg, err)
		}
	}
	code, out, err := captureRun(t, []string{"kander", "install", "--help"})
	if code != 0 || !strings.Contains(out, "kander install") || err != "" {
		t.Fatalf("help code=%d out=%q err=%q", code, out, err)
	}
}

func TestDefaultRunner(t *testing.T) {
	resetLang(t)
	old := DefaultRunner
	t.Cleanup(func() { DefaultRunner = old })
	called := false
	DefaultRunner = func() int {
		called = true
		return 7
	}
	if code := Run([]string{"kander"}); code != 7 || !called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}

func TestUninstallCommandRegistered(t *testing.T) {
	resetLang(t)
	code, out, diagnostic := captureRun(t, []string{"kander", "--lang", "en", "uninstall", "--help"})
	if code != 0 || !onlyFallbackHint(diagnostic) || !strings.Contains(out, "kander uninstall [--yes]") || !strings.Contains(out, "snippets/commands/uninstall.md") {
		t.Fatalf("code=%d out=%q err=%q", code, out, diagnostic)
	}
	code, out, _ = captureRun(t, []string{"kander", "--lang", "en", "help"})
	if code != 0 || !strings.Contains(out, "  uninstall\n") {
		t.Fatalf("help does not list uninstall: code=%d out=%q", code, out)
	}
}

func TestTerminalCommandRegistered(t *testing.T) {
	resetLang(t)
	for _, language := range []string{"cn", "en", "ja"} {
		code, out, diagnostic := captureRun(t, []string{"kander", "--lang", language, "terminal", "--help"})
		if code != 0 || !onlyFallbackHint(diagnostic) || !strings.Contains(out, "kander terminal list") || !strings.Contains(out, "--skip-focus") || !strings.Contains(out, "snippets/commands/terminal.md") {
			t.Fatalf("language=%s code=%d out=%q err=%q", language, code, out, diagnostic)
		}
	}
}

// onlyFallbackHint accepts an empty stderr, or the once-per-file hint that the installed
// contract is missing (printed on a machine whose rules root predates snippets/commands).
func onlyFallbackHint(stderr string) bool {
	return stderr == "" || strings.Contains(stderr, "kander doctor")
}
