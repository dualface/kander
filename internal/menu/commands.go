package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	var jsonOut bool
	c := &cobra.Command{
		Use:  "config",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runConfig(jsonOut)) },
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "")
	return c
}

// Doctor implements kander doctor.
func Doctor() int {
	prepareLanguage()
	// printDoctorWithTools runs the version guard first, so an older binary
	// stops before the herdr install prompt and every repair.
	if printDoctorWithTools(CheckTerminalTools(), true, stdinStderrTTY()) {
		return 0
	}
	return 1
}

func runConfig(jsonOut bool) int {
	prepareLanguage()
	cfg, err := config.Load(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander:", err)
		return 1
	}
	if jsonOut {
		for _, warning := range config.AgentWarnings(cfg) {
			fmt.Fprintln(os.Stderr, warning)
		}
		payload, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "kander:", err)
			return 1
		}
		fmt.Println(string(payload))
		return 0
	}
	if err := printConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "kander:", err)
		return 1
	}
	return 0
}
