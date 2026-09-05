//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalToolsProbe(t *testing.T) {
	for _, tc := range []struct {
		name, herdr, tmux string
		wantInstall       bool
	}{
		{"missing", "", "", true},
		{"herdr", "echo herdr-version", "", false},
		{"tmux", "", "echo tmux-version", false},
		{"broken", "exit 1", "exit 1", true},
		{"empty", "exit 0", "exit 0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			t.Setenv("PATH", h.fakeBin)
			for name, script := range map[string]string{"herdr": tc.herdr, "tmux": tc.tmux} {
				if script != "" {
					flag := "--version"
					if name == "tmux" {
						flag = "-V"
					}
					h.fakeCommand(name, "#!/bin/sh\n[ \"$1\" = \""+flag+"\" ] || exit 2\n"+script+"\n")
				}
			}
			if got := CheckTerminalTools(); got.NeedsHerdrInstall() != tc.wantInstall {
				t.Fatalf("tools=%+v, want recommendation=%v", got, tc.wantInstall)
			}
		})
	}
}

func TestTerminalToolProbeTimeout(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	h.fakeCommand("herdr", "#!/bin/sh\nexec /bin/sleep 30\n")
	started := time.Now()
	result := probeTerminalTool("herdr", "--version")
	if result.Available() || result.Error == "" || time.Since(started) > 10*time.Second {
		t.Fatalf("probe must time out: %+v, elapsed=%v", result, time.Since(started))
	}
}

func TestDoctorHerdrInstallConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, answers, curl string
		tty, tmux, run      bool
		message             string
	}{
		{"noninteractive", "", "exit 9", false, false, false, "推荐安装 herdr"},
		{"decline", "2", "exit 9", true, false, false, "Agent 能力"},
		{"default-skip", "", "exit 9", true, false, false, "Agent 能力"},
		{"tmux-available", "", "exit 9", true, true, false, "Agent 能力"},
		{"download-fails", "1", "exit 22", true, false, true, "herdr 安装失败"},
		{"installer-fails", "1", "printf '%s\\n' 'exit 9'", true, false, true, "herdr 安装失败"},
		{"installed-outside-path", "1", "printf '%s\\n' 'exit 0'", true, false, true, "当前进程仍无法使用 herdr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.installFake(tc.tmux)
			h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground"}))
			marker := filepath.Join(h.root, "installer-ran")
			h.setenv("INSTALL_MARKER", marker)
			h.fakeCommand("curl", "#!/bin/sh\n[ \"$1\" = -fsSL ] && [ \"$2\" = https://herdr.dev/install.sh ] || exit 3\n: > \"$INSTALL_MARKER\"\n"+tc.curl+"\n")
			if err := os.Symlink("/bin/sh", filepath.Join(h.fakeBin, "sh")); err != nil {
				t.Fatal(err)
			}
			var code int
			var out string
			if tc.tty {
				answers := tc.answers
				if tc.name == "default-skip" {
					answers = "\n"
				}
				code, out = h.runOnTTY(answers, "doctor")
			} else {
				code, _, out = h.run("doctor")
			}
			if code != 0 || !strings.Contains(out, tc.message) || !strings.Contains(out, "配置:") {
				t.Fatalf("doctor did not continue: code=%d output=%s", code, out)
			}
			_, err := os.Stat(marker)
			if (err == nil) != tc.run {
				t.Fatalf("installer ran=%v, want %v", err == nil, tc.run)
			}
		})
	}
}

func TestHerdrInstallRechecksAvailability(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	t.Setenv("TEST_HERDR_BIN", filepath.Join(h.fakeBin, "herdr"))
	if err := os.Symlink("/bin/sh", filepath.Join(h.fakeBin, "sh")); err != nil {
		t.Fatal(err)
	}
	h.fakeCommand("curl", "#!/bin/sh\nprintf '%s\\n' 'printf \"#!/bin/sh\\necho herdr-version\\n\" > \"$TEST_HERDR_BIN\"' '/bin/chmod +x \"$TEST_HERDR_BIN\"'\n")
	lines, ok := (&Session{}).InstallHerdr()
	if !ok || !CheckTerminalTools().Herdr.Available() {
		t.Fatalf("installation failed: %+v", lines)
	}
}

func TestHerdrWindowsInstallerArguments(t *testing.T) {
	h := newHarness(t)
	t.Setenv("PATH", h.fakeBin)
	record := filepath.Join(h.root, "argv")
	t.Setenv("INSTALL_ARGS", record)
	h.fakeCommand("powershell", "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$INSTALL_ARGS\"\nexit 7\n")
	previous := windowsOS
	windowsOS = true
	t.Cleanup(func() { windowsOS = previous })
	lines, ok := (&Session{}).InstallHerdr()
	if ok {
		t.Fatal("installer failure reported as success")
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "-ExecutionPolicy\nBypass\n-c\nirm https://herdr.dev/install.ps1 | iex\n" {
		t.Fatalf("argv: %q", data)
	}
	if !strings.Contains(lines[len(lines)-1].Text, "herdr") {
		t.Fatalf("missing failure report: %+v", lines)
	}
}
