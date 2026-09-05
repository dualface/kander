package menu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
)

// TerminalTool 是一次有超时的命令可用性探测结果.
type TerminalTool struct {
	Path  string
	Error string
}

func (t TerminalTool) Available() bool { return t.Path != "" && t.Error == "" }

// TerminalTools 供 doctor 与 Settings 共用, 不修改 launcher 配置.
type TerminalTools struct {
	Herdr TerminalTool
	Tmux  TerminalTool
}

func (t TerminalTools) NeedsHerdrInstall() bool {
	return !t.Herdr.Available() && !t.Tmux.Available()
}

func probeTerminalTool(name, flag string) TerminalTool {
	result := TerminalTool{Path: lookPath(name)}
	if result.Path == "" {
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

// CheckTerminalTools 只探测命令, 不启动 session/window/tab, 也不安装软件.
func CheckTerminalTools() TerminalTools {
	return TerminalTools{Herdr: probeTerminalTool("herdr", "--version"), Tmux: probeTerminalTool("tmux", "-V")}
}

func HerdrInstallPrompt() string {
	return config.Text("menu.neither_herdr_nor_tmux_is_available_we_recommend_herdr")
}

func HerdrInstallCommand() string {
	if isWindowsOS() {
		return `powershell -ExecutionPolicy Bypass -c "irm https://herdr.dev/install.ps1 | iex"`
	}
	return "curl -fsSL https://herdr.dev/install.sh | sh"
}

// InstallHerdr 在用户确认后调用官方安装器; TUI 必须先挂起终端.
// 失败以报告返回, 调用方继续后续检查, 不修改配置或当前进程 PATH.
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
		if !probeTerminalTool("herdr", "--version").Available() {
			hint(config.Text("menu.herdr_is_still_unavailable_to_this_process_check_path"))
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
	// 分别等待管道两端, 避免 curl 失败被 sh 对空输入的成功退出掩盖.
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
	for _, item := range []struct {
		name string
		tool TerminalTool
	}{{"herdr", tools.Herdr}, {"tmux", tools.Tmux}} {
		switch {
		case item.tool.Available():
			success(item.name + ": " + item.tool.Path)
		case item.tool.Path != "":
			warning(item.name + ": " + item.tool.Error)
		default:
			hint(config.Text("menu.not_installed", item.name))
		}
	}
	if tools.NeedsHerdrInstall() {
		hint(config.Text("menu.neither_herdr_nor_tmux_is_available_we_recommend_installing") + HerdrInstallCommand())
	}
}
