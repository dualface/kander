//go:build linux

package tui

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// ptySession runs kander inside a pseudo-terminal and answers capability queries like a real terminal would,
// otherwise Bubble Tea keeps waiting for the background color and cursor position responses.
type ptySession struct {
	t      *testing.T
	cmd    *exec.Cmd
	master *os.File
	mu     sync.Mutex
	out    []byte
	done   chan error
}

func startPTY(t *testing.T, bin string, env []string, args ...string) *ptySession {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Fatal(err)
	}
	ws := unix.Winsize{Row: 32, Col: 120}
	_ = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &ws)
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	session := &ptySession{t: t, cmd: cmd, master: master, done: make(chan error, 1)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				session.mu.Lock()
				session.out = append(session.out, chunk...)
				session.mu.Unlock()
				if bytes.Contains(chunk, []byte("\x1b]11;?")) {
					_, _ = master.Write([]byte("\x1b]11;rgb:0000/0000/0000\x1b\\"))
				}
				if bytes.Contains(chunk, []byte("\x1b[6n")) {
					_, _ = master.Write([]byte("\x1b[1;1R"))
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { session.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = master.Close()
	})
	return session
}

func (s *ptySession) send(keys string) {
	s.t.Helper()
	if _, err := s.master.Write([]byte(keys)); err != nil {
		s.t.Fatal(err)
	}
}

func (s *ptySession) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.out)
}

// waitFor waits for some text to appear in the pseudo-terminal output.
func (s *ptySession) waitFor(needle string, timeout time.Duration) bool {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if bytes.Contains([]byte(s.text()), []byte(needle)) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (s *ptySession) waitExit(timeout time.Duration) error {
	s.t.Helper()
	select {
	case err := <-s.done:
		return err
	case <-time.After(timeout):
		s.t.Fatalf("process did not exit\npty:\n%s", s.text())
		return nil
	}
}

func buildKander(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kander")
	build := exec.Command("go", "build", "-o", bin, "./cmd/kander")
	build.Dir = moduleRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// isolatedHome gives the child process its own HOME and config path and drops in a fake codex,
// so the test neither reads nor writes the user's real Kander config.
func isolatedHome(t *testing.T) (home string, env []string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{home, fakeBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nprintf '%s\\n' 'codex test-version'\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env = []string{
		"TERM=xterm-256color",
		"PATH=" + fakeBin + ":" + os.Getenv("PATH"),
		"HOME=" + home,
		"USERPROFILE=" + home,
		"KANDER_CONFIG=" + filepath.Join(home, ".config", "kander", "config.json"),
		"KANDER_LANG=en",
		"KANDER_SKIP_INSTALL=1",
	}
	return home, env
}

func boardEnv(t *testing.T) (string, []string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "kanban")
	t.Setenv(board.EnvBoardDir, root)
	if _, _, _, err := board.InitBoard(""); err != nil {
		t.Fatal(err)
	}
	_, env := isolatedHome(t)
	return root, append(env, board.EnvBoardDir+"="+root)
}

func configPathFromEnv(t *testing.T, env []string) string {
	t.Helper()
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, config.EnvConfig+"="); ok {
			return value
		}
	}
	t.Fatal("config path missing from environment")
	return ""
}

func writeCompleteConfig(t *testing.T, env []string) {
	t.Helper()
	path := configPathFromEnv(t, env)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A bare first launch runs doctor, then enters the board and opens the interface options automatically.
func TestBareKanderBootstrapsConfigAndOpensInterfaceOptionsOnPTY(t *testing.T) {
	bin := buildKander(t)
	_, env := boardEnv(t)
	session := startPTY(t, bin, env)
	if !session.waitFor("Task Board", 8*time.Second) {
		t.Fatalf("board did not render\npty:\n%s", session.text())
	}
	if !session.waitFor("Default language", 10*time.Second) {
		t.Fatalf("interface options did not open\npty:\n%s", session.text())
	}
	configPath := configPathFromEnv(t, env)
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("doctor did not create config: %v", err)
	}
	session.send("\r")
	if !session.waitFor("Save and apply", 4*time.Second) {
		t.Fatalf("interface did not return to options root\npty:\n%s", session.text())
	}
	session.send("q")
	time.Sleep(300 * time.Millisecond)
	session.send("q")
	if err := session.waitExit(8 * time.Second); err != nil {
		t.Fatalf("exit: %v\npty:\n%s", err, session.text())
	}
	stored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(stored, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.TUI != config.DefaultTUI() {
		t.Fatalf("opening interface changed defaults: %+v", cfg.TUI)
	}
}

// Press o on the board to open the options panel.
func TestBoardOpensOptionsPanelOnPTY(t *testing.T) {
	bin := buildKander(t)
	_, env := boardEnv(t)
	writeCompleteConfig(t, env)
	session := startPTY(t, bin, env)
	if !session.waitFor("Task Board", 8*time.Second) {
		t.Fatalf("board did not render\npty:\n%s", session.text())
	}
	session.send("o")
	if !session.waitFor("Save and apply", 10*time.Second) {
		t.Fatalf("options panel did not open\npty:\n%s", session.text())
	}
	// A gap is required between Esc and the following key, otherwise the terminal parses them as alt+<key>.
	session.send("\x1b")
	time.Sleep(300 * time.Millisecond)
	session.send("q")
	if err := session.waitExit(8 * time.Second); err != nil {
		t.Fatalf("exit: %v\npty:\n%s", err, session.text())
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("go.mod not found")
		}
		wd = parent
	}
}

func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
