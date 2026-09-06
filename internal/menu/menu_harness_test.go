//go:build unix

package menu

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

var testKander string
var repoRoot string

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		os.Exit(1)
	}
	repoRoot = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	dir, err := os.MkdirTemp("", "kander-menu-bin")
	if err != nil {
		os.Exit(1)
	}
	testKander = filepath.Join(dir, "kander")
	cmd := exec.Command("go", "build", "-o", testKander, "./cmd/kander")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func envWith(home, configPath, path string) []string {
	env := os.Environ()
	blocked := map[string]struct{}{
		"KANDER_LANG": {}, "KANDER_LANG_CLI": {}, "KANDER_CONFIG": {},
		"HOME": {}, "PATH": {}, "NO_COLOR": {}, "LC_ALL": {}, "LC_MESSAGES": {}, "LANG": {},
		"HERDR_ENV": {}, "HERDR_WORKSPACE_ID": {}, "HERDR_TAB_ID": {}, "HERDR_PANE_ID": {},
		"TMUX": {}, "GIT_DIR": {}, "GIT_WORK_TREE": {}, "GIT_COMMON_DIR": {},
	}
	out := make([]string, 0, len(env)+8)
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		if _, ok := blocked[name]; ok {
			continue
		}
		out = append(out, item)
	}
	out = append(out,
		"HOME="+home,
		"USERPROFILE="+home,
		"KANDER_CONFIG="+configPath,
		"KANDER_LANG=cn",
		"PATH="+path,
	)
	return out
}

type harness struct {
	t          *testing.T
	root       string
	home       string
	configPath string
	fakeBin    string
	env        []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(testKander, filepath.Join(fakeBin, "kander"), 0o755); err != nil {
		t.Fatal(err)
	}
	// In-process probes (the herdr default install directory and friends) also
	// read HOME/USERPROFILE/LOCALAPPDATA, so isolate those to the temp dir too
	// instead of letting them reach the developer's real home.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	cfg := filepath.Join(home, ".config", "kander", "config.json")
	h := &harness{
		t: t, root: root, home: home, configPath: cfg, fakeBin: fakeBin,
		env: envWith(home, cfg, fakeBin),
	}
	return h
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (h *harness) fakeCommand(name, body string) {
	h.t.Helper()
	if body == "" {
		body = "#!/bin/sh\nprintf '%s\\n' '" + name + " test-version'\n"
	}
	path := filepath.Join(h.fakeBin, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) installFake(tmux bool) {
	for _, name := range []string{"codex", "claude", "grok"} {
		h.fakeCommand(name, "")
	}
	if tmux {
		h.fakeCommand("tmux", "")
	}
}

func (h *harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	cmd := exec.Command(testKander, args...)
	cmd.Env = h.env
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			h.t.Fatal(err)
		}
	}
	return code, stdout.String(), stderr.String()
}

func (h *harness) setenv(key, value string) {
	found := false
	for i, item := range h.env {
		if strings.HasPrefix(item, key+"=") {
			h.env[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		h.env = append(h.env, key+"="+value)
	}
}

func (h *harness) unsetenv(key string) {
	out := h.env[:0]
	for _, item := range h.env {
		if !strings.HasPrefix(item, key+"=") {
			out = append(out, item)
		}
	}
	h.env = out
}

func (h *harness) writeConfig(payload map[string]any) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
		h.t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.configPath, data, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func defaultPayload(overrides map[string]any) map[string]any {
	payload := map[string]any{
		"schema_version":   1,
		"welcome_complete": true,
		"kanban_agent":     "codex",
		"launcher":         "tmux",
		"reviewers": map[string]any{
			"PM": "codex", "CSA": "codex", "Hacker": "codex", "QA": "codex",
		},
	}
	for k, v := range overrides {
		payload[k] = v
	}
	return payload
}

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	fd := int(master.Fd())
	var unlock int32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCSPTLCK), uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	var n uint32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCGPTN), uintptr(unsafe.Pointer(&n))); errno != 0 {
		master.Close()
		t.Fatal(errno)
	}
	name := "/dev/pts/" + strconv.FormatUint(uint64(n), 10)
	slave, err = os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	ws := unix.Winsize{Row: 40, Col: 120}
	_ = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &ws)
	return master, slave
}

func ptsName(master *os.File) (string, error) {
	var n uint32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(master.Fd()), uintptr(unix.TIOCGPTN), uintptr(unsafe.Pointer(&n))); errno != 0 {
		return "", errno
	}
	return "/dev/pts/" + strconv.FormatUint(uint64(n), 10), nil
}

func (h *harness) runOnTTY(answers string, args ...string) (int, string) {
	h.t.Helper()
	return h.runBinOnTTY(testKander, answers, args...)
}

func (h *harness) runBinOnTTY(bin, answers string, args ...string) (int, string) {
	h.t.Helper()
	master, slave := openPTY(h.t)
	cmd := exec.Command(bin, args...)
	cmd.Env = append(append([]string{}, h.env...), "TERM=xterm-256color")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		slave.Close()
		master.Close()
		h.t.Fatal(err)
	}
	slave.Close()
	var seen []byte
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				seen = append(seen, buf[:n]...)
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	hidden := 0
	waitMenu := func() bool {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			joined := string(seen)
			mu.Unlock()
			count := strings.Count(joined, "\033[?25l")
			if count > hidden {
				hidden = count
				return true
			}
			if strings.Contains(joined, "Choose [1-") || strings.Contains(joined, "请选择 [1-") {
				return true
			}
			time.Sleep(30 * time.Millisecond)
		}
		return false
	}
	if answers != "" {
		if !waitMenu() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			master.Close()
			<-done
			mu.Lock()
			out := string(seen)
			mu.Unlock()
			h.t.Fatalf("interactive menu did not appear: %s", out)
		}
		lines := strings.Split(answers, "\n")
		for i, line := range lines {
			_, _ = master.Write([]byte(line + "\n"))
			if i < len(lines)-1 {
				_ = waitMenu()
			}
			time.Sleep(40 * time.Millisecond)
		}
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var err error
	select {
	case err = <-waited:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		<-waited
		master.Close()
		<-done
		mu.Lock()
		out := string(seen)
		mu.Unlock()
		h.t.Fatalf("timeout: %s", out)
	}
	master.Close()
	<-done
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			h.t.Fatal(err)
		}
	}
	mu.Lock()
	out := string(seen)
	mu.Unlock()
	return code, out
}
