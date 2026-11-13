package menu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dualface/kander/internal/client"
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

var errHelp = errors.New("help")

func parseConfigArgs(args []string) (jsonOut bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		case "-h", "--help":
			fmt.Println(config.CommandUsage("config"))
			return false, errHelp
		default:
			return false, errors.New(config.Text("board.unknown_option", arg))
		}
	}
	return jsonOut, nil
}

// Doctor implements kander doctor.
func Doctor(args []string) int {
	prepareLanguage()
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Println(config.CommandUsage("doctor"))
			return 0
		}
		fmt.Fprintln(os.Stderr, "kander:", config.Text("board.unknown_option", arg))
		return 2
	}
	// printDoctorWithTools runs the version guard first, so an older binary
	// stops before the herdr install prompt and every repair.
	if printDoctorWithTools(CheckTerminalTools(), true, stdinStderrTTY()) {
		return 0
	}
	return 1
}

// Config implements kander config [--json].
func Config(args []string) int {
	prepareLanguage()
	jsonOut, err := parseConfigArgs(args)
	if err != nil {
		if errors.Is(err, errHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "kander:", err)
		return 2
	}
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
