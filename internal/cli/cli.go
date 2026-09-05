// Package cli 注册全部 kander 子命令, 解析全局 --lang, 并分发到各实现包.
//
// 本文件的命令表一次注册完毕. 后续实现卡只覆写对应 Runner, 不要为接线
// 改命令名列表或 main.go.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
	"github.com/dualface/kander/internal/version"
)

// Runner 是子命令入口. args 不含程序名和子命令名.
type Runner func(args []string) int

var commandNames = []string{
	"doctor",
	"config",
	"version",
	"review",
	"init",
	"list",
	"show",
	"new",
	"move",
	"pick",
	"start",
	"resume",
	"notify",
	"dismiss",
	"check",
	"guard-write",
	"subscribe",
}

var aliases = map[string]string{
	"ls": "list",
}

// Commands 是冻结的注册表. 缺省全部为未实现; 后续包可覆写同名项.
var Commands = map[string]Runner{}

// DefaultRunner 是不带子命令时要运行的入口. internal/tui 在注册时设置它,
// 于是裸 kander 直接进入 alt-screen 看板; 为空则打印用法.
var DefaultRunner Runner

func init() {
	for _, name := range commandNames {
		Commands[name] = Unimplemented(name)
	}
	Commands["doctor"] = menu.Doctor
	Commands["config"] = menu.Config
	Commands["version"] = runVersion
}

func runVersion(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(os.Stdout, config.Text("cli.version_usage"))
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, config.Text("cli.version_usage"))
		return 2
	}
	fmt.Fprintln(os.Stdout, "kander "+version.String())
	return 0
}

// Unimplemented 返回稳定非零退出的占位实现, 不 panic.
func Unimplemented(name string) Runner {
	return func(args []string) int {
		fmt.Fprintln(os.Stderr, unimplementedMessage(name))
		return 1
	}
}

func unimplementedMessage(name string) string {
	return config.Text(
		"cli.subcommand_is_not_implemented", name,
	)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, config.Text("cli.usage"))
	fmt.Fprintln(w, config.Text("cli.commands"))
	for _, name := range commandNames {
		label := name
		if name == "list" {
			label = "list / ls"
		}
		fmt.Fprintf(w, "  %s\n", label)
	}
	fmt.Fprintln(w, config.Text("cli.global_options"))
}

type parsedArgs struct {
	help    bool
	command string
	rest    []string
	langErr string
}

func parseArgs(arguments []string) parsedArgs {
	var parsed parsedArgs
	args := arguments
	if len(args) > 0 {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if parsed.command == "" && i+1 < len(args) {
				parsed.command = args[i+1]
				parsed.rest = append([]string{}, args[i+2:]...)
			} else {
				parsed.rest = append(parsed.rest, args[i+1:]...)
			}
			return parsed
		}
		if arg == "-h" || arg == "--help" {
			if parsed.command == "" {
				parsed.help = true
				continue
			}
			parsed.rest = append(parsed.rest, arg)
			continue
		}
		if arg == "--lang" {
			if i+1 >= len(args) {
				parsed.langErr = "missing"
				return parsed
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "--lang=") {
			continue
		}
		if parsed.command == "" {
			if strings.HasPrefix(arg, "-") {
				parsed.langErr = "unknown:" + arg
				return parsed
			}
			parsed.command = arg
			continue
		}
		parsed.rest = append(parsed.rest, arg)
	}
	return parsed
}

func validateLang(arguments []string) string {
	args := arguments
	if len(args) > 0 {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value string
		switch {
		case arg == "--lang":
			if i+1 >= len(args) {
				return "missing"
			}
			value = args[i+1]
			i++
		case strings.HasPrefix(arg, "--lang="):
			value = strings.TrimPrefix(arg, "--lang=")
		default:
			continue
		}
		if !containsString(config.Languages, value) {
			return value
		}
	}
	return ""
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func failUsage(message string) int {
	usage(os.Stderr)
	if message != "" {
		fmt.Fprintln(os.Stderr, message)
	}
	return 2
}

func langErrorMessage(kind string) string {
	if kind == "missing" {
		return config.Text("cli.error_lang_must_be_cn_or_en")
	}
	if strings.HasPrefix(kind, "unknown:") {
		return config.Text(
			"cli.error_unknown_option", strings.TrimPrefix(kind, "unknown:"),
		)
	}
	return config.Text("cli.error_lang_must_be_cn_or_en")
}

func unknownCommandMessage(name string) string {
	return config.Text("cli.error_unknown_command", name)
}

// Run 解析 os.Args 形态的参数并分发. 返回进程退出码.
func Run(arguments []string) int {
	langKind := validateLang(arguments)
	config.ApplyLanguageArgument(arguments)
	if langKind != "" {
		return failUsage(langErrorMessage(langKind))
	}
	parsed := parseArgs(arguments)
	if parsed.langErr != "" {
		return failUsage(langErrorMessage(parsed.langErr))
	}
	if parsed.help && parsed.command == "" {
		usage(os.Stdout)
		return 0
	}
	if parsed.command == "" {
		if DefaultRunner != nil {
			return DefaultRunner(parsed.rest)
		}
		return failUsage("")
	}
	if parsed.command == "help" {
		usage(os.Stdout)
		return 0
	}
	name := parsed.command
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	runner, ok := Commands[name]
	if !ok {
		return failUsage(unknownCommandMessage(parsed.command))
	}
	return runner(parsed.rest)
}
