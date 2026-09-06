package menu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
)

// TerminalTool is the result of one command availability probe with a timeout.
// OffPath records an executable found in the official installer's default
// directory but not on PATH: the installer's PATH edit only reaches terminals
// opened afterwards, so the current process cannot see it.
type TerminalTool struct {
	Path    string
	OffPath string
	Error   string
}

func (t TerminalTool) Available() bool { return t.Path != "" && t.Error == "" }

// Installed reports that this machine has the tool even when the current
// process's PATH cannot see it yet. doctor uses this so it does not rewrite the
// user's launcher: telling them to reopen the terminal beats silently
// switching their config.
func (t TerminalTool) Installed() bool { return t.Available() || t.OffPath != "" }

// TerminalTools is shared by doctor and Settings; it never modifies the launcher config.
type TerminalTools struct {
	Herdr TerminalTool
	Tmux  TerminalTool
}

func (t TerminalTools) NeedsHerdrInstall() bool {
	return !t.Herdr.Available() && t.Herdr.OffPath == "" && !t.Tmux.Available()
}

// herdrDefaultBinaries lists where the official herdr installer puts the
// binary, most preferred first.
func herdrDefaultBinaries() []string {
	if isWindowsOS() {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return nil
		}
		return []string{filepath.Join(local, "Programs", "Herdr", "bin", "herdr.exe")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin", "herdr")}
}

func defaultToolBinary(name string) string {
	if name != "herdr" {
		return ""
	}
	for _, candidate := range herdrDefaultBinaries() {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func probeTerminalTool(name, flag string) TerminalTool {
	result := TerminalTool{Path: lookPath(name)}
	if result.Path == "" {
		result.OffPath = defaultToolBinary(name)
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, result.Path, flag)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		result.Error = flag + ": " + err.Error()
	} else if strings.TrimSpace(string(out)) == "" {
		result.Error = config.Text("menu.empty_version_output")
	}
	return result
}

// CheckTerminalTools only probes commands: it starts no session/window/tab and installs no software.
// Native Windows has no tmux, so it is neither probed nor reported there.
func CheckTerminalTools() TerminalTools {
	tools := TerminalTools{Herdr: probeTerminalTool("herdr", "--version")}
	if !isWindowsOS() {
		tools.Tmux = probeTerminalTool("tmux", "-V")
	}
	return tools
}

func HerdrInstallPrompt() string {
	if isWindowsOS() {
		return config.Text("menu.herdr_is_not_available_we_recommend_herdr")
	}
	return config.Text("menu.neither_herdr_nor_tmux_is_available_we_recommend_herdr")
}

func HerdrInstallCommand() string {
	if isWindowsOS() {
		return `powershell -ExecutionPolicy Bypass -c "irm https://herdr.dev/install.ps1 | iex"`
	}
	return "curl -fsSL https://herdr.dev/install.sh | sh"
}

// InstallHerdr runs the official installer once the user confirms; the TUI must suspend the terminal first.
// Failures come back as report lines, the caller continues with the remaining checks, and neither the config nor the process PATH is modified.
func (s *Session) InstallHerdr() ([]ReportLine, bool) {
	installed := false
	lines := CaptureReport(func() {
		hint(config.Text("menu.about_to_run") + HerdrInstallCommand())
		if err := runHerdrInstaller(); err != nil {
			warning(config.Text("menu.herdr_installation_failed_continuing") + err.Error())
			return
		}
		installed = true
		success(config.Text("menu.herdr_installer_completed"))
		// The installer edits PATH in the registry or a shell rc, which the current
		// process cannot see. When the default directory has it, report the exact
		// path instead of a vague "still unavailable".
		if probed := probeTerminalTool("herdr", "--version"); !probed.Available() {
			if probed.OffPath != "" {
				hint(config.Text("menu.herdr_installed_at_reopen_terminal", probed.OffPath))
			} else {
				hint(config.Text("menu.herdr_is_still_unavailable_to_this_process_check_path"))
			}
		}
	})
	return lines, installed
}

func runHerdrInstaller() error {
	if isWindowsOS() {
		cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-c", "irm https://herdr.dev/install.ps1 | iex")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	// Wait on both ends of the pipe separately, so a curl failure is not masked by sh exiting successfully on empty input.
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	shell := exec.Command("sh")
	shell.Stdin, shell.Stdout, shell.Stderr = reader, os.Stdout, os.Stderr
	if err := shell.Start(); err != nil {
		return err
	}
	reader.Close()
	curl := exec.Command("curl", "-fsSL", "https://herdr.dev/install.sh")
	curl.Stdout, curl.Stderr = writer, os.Stderr
	downloadErr := curl.Run()
	writer.Close()
	shellErr := shell.Wait()
	if downloadErr != nil {
		return fmt.Errorf("curl: %w", downloadErr)
	}
	return shellErr
}

func offerHerdrInstall(tools TerminalTools) TerminalTools {
	if !tools.NeedsHerdrInstall() || !stdinStderrTTY() {
		return tools
	}
	hint(HerdrInstallCommand())
	selected, err := askChoice(HerdrInstallPrompt(), []choice{
		{Value: "install", Label: config.Text("menu.install_herdr")},
		{Value: "skip", Label: config.Text("menu.skip_and_continue_checking")},
	}, "skip")
	if err != nil || selected != "install" {
		return tools
	}
	session := &Session{}
	lines, _ := session.InstallHerdr()
	FlushReport(lines)
	return CheckTerminalTools()
}

func reportTerminalTools(tools TerminalTools) {
	report := func(name string, tool TerminalTool) {
		switch {
		case tool.Available():
			success(name + ": " + tool.Path)
		case tool.Path != "":
			warning(name + ": " + tool.Error)
		case tool.OffPath != "":
			warning(config.Text("menu.installed_but_not_in_path", name, tool.OffPath))
		default:
			hint(config.Text("menu.not_installed", name))
		}
	}
	report("herdr", tools.Herdr)
	if !isWindowsOS() {
		report("tmux", tools.Tmux)
	}
	if tools.NeedsHerdrInstall() {
		if isWindowsOS() {
			hint(config.Text("menu.herdr_is_not_available_we_recommend_installing") + HerdrInstallCommand())
		} else {
			hint(config.Text("menu.neither_herdr_nor_tmux_is_available_we_recommend_installing") + HerdrInstallCommand())
		}
	}
}
