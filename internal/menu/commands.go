package menu

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

func prepareLanguage() {
	config.BindEffectiveLanguage()
}

// configBoardTimeout bounds the board path lookup of the human-readable
// kander config output, including the client's transport retries.
const configBoardTimeout = 3 * time.Second

// configClientID identifies the lookup; kander config never reads or creates
// the client home's client-id, and the server does not check it on this route.
const configClientID = "config-readonly"

// serverBoardPath asks the bound server for the project's board path. It sends
// only GET and writes nothing; every failure reads as unavailable.
func serverBoardPath(cfg *config.Config) string {
	if cfg == nil || cfg.Server.URL == "" || cfg.Project.Name == "" {
		return ""
	}
	c, err := client.New(cfg.Server.URL, configClientID)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), configBoardTimeout)
	defer cancel()
	info, err := c.ProjectByName(ctx, cfg.Project.Name)
	if err != nil {
		return ""
	}
	return info.BoardPath
}

func printConfig(cfg *config.Config) error {
	lines, err := config.FormatConfigLines(cfg, serverBoardPath(cfg))
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

// NewDoctorCommand builds kander doctor.
func NewDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "doctor",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(Doctor()) },
	}
}

// NewConfigCommand builds kander config.
func NewConfigCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "config",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runConfig()) },
	}
}

// Doctor implements kander doctor.
func Doctor() int {
	prepareLanguage()
	if command.JSON() {
		return doctorJSON()
	}
	// printDoctorWithTools runs the version guard first, so an older binary
	// stops before the herdr install prompt and every repair.
	if printDoctorWithTools(CheckTerminalTools(), true, stdinStderrTTY()) {
		return 0
	}
	return 1
}

func runConfig() int {
	prepareLanguage()
	cfg, err := config.Load(false)
	if err != nil {
		return command.Fail(err)
	}
	if command.JSON() {
		for _, warning := range config.AgentWarnings(cfg) {
			command.Notify("agent_warning", warning)
		}
		return command.Result(cfg)
	}
	if err := printConfig(cfg); err != nil {
		return command.Fail(err)
	}
	return 0
}

// doctorLine is one report line of the result of kander doctor.
type doctorLine struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// doctorJSON is doctor with --json: the same checks and repairs, never a prompt, with the report
// lines in the result. A run that an interactive doctor would have asked something in (a
// repair that needs a confirmation) is refused as an interactive form before any repair. An
// unhealthy installation is the result with healthy false and exit status 1.
func doctorJSON() int {
	tools := CheckTerminalTools()
	binaries := pathKanderBinaries()
	// The version guard comes first: a running binary older than one on PATH stops doctor with
	// its own diagnostic, which the person needs more than a refusal to repair.
	if !runningKanderIsOutdated(binaries) {
		if reasons := confirmationsOf(tools, binaries); len(reasons) > 0 {
			return command.FailStatus(2, refuseDoctorConfirmations(reasons))
		}
	}
	healthy := false
	captured := CaptureReport(func() {
		healthy = printDoctorWithBinaries(tools, true, false, binaries)
	})
	lines := make([]doctorLine, 0, len(captured))
	for _, line := range captured {
		lines = append(lines, doctorLine{Level: line.Level, Message: line.Text})
	}
	status := 0
	if !healthy {
		status = 1
	}
	return command.ResultStatus(struct {
		Healthy bool         `json:"healthy"`
		Lines   []doctorLine `json:"lines"`
	}{healthy, lines}, status)
}
