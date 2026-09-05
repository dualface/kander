package menu

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/config"
)

func prepareLanguage() {
	config.BindEffectiveLanguage()
}

func printConfig(cfg *config.Config) error {
	lines, err := config.FormatConfigLines(cfg)
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
			fmt.Println(config.Text("menu.usage_kander_config_json"))
			return false, errHelp
		default:
			return false, errors.New(config.Text("board.unknown_option", arg))
		}
	}
	return jsonOut, nil
}

// Doctor 实现 kander doctor.
func Doctor(args []string) int {
	prepareLanguage()
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Println(config.Text("menu.usage_kander_doctor"))
			return 0
		}
		fmt.Fprintln(os.Stderr, "kander:", config.Text("board.unknown_option", arg))
		return 2
	}
	if printDoctorWithTools(offerHerdrInstall(CheckTerminalTools()), true) {
		return 0
	}
	return 1
}

// Config 实现 kander config [--json].
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
	cfg, err := config.Load(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander:", err)
		return 1
	}
	if jsonOut {
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
