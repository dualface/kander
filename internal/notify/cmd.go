package notify

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
)

func init() {
	cli.Commands["notify"] = RunNotify
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

func usage(w io.Writer) {
	fmt.Fprintln(w, t(
		"notify.usage_kander_notify_pane_herdr_pane_id_timeout_seconds",
	))
}

func usageFail(id string, args ...any) int {
	usage(os.Stderr)
	if id != "" {
		fmt.Fprintln(os.Stderr, t(id, args...))
	}
	return 2
}

func takeValue(args []string, i int) (string, int, bool) {
	if i+1 >= len(args) {
		return "", i, false
	}
	return args[i+1], i + 1, true
}

// RunNotify 实现 kander notify.
func RunNotify(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage(os.Stdout)
			return 0
		}
	}
	timeout := 120.0
	var message, messageFile, pane string
	var messageSet bool
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--message":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("launch.missing_message_value")
			}
			message, messageSet, i = val, true, next
		case strings.HasPrefix(arg, "--message="):
			message, messageSet = strings.TrimPrefix(arg, "--message="), true
		case arg == "--message-file":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("launch.missing_message_file_value")
			}
			messageFile, i = val, next
		case strings.HasPrefix(arg, "--message-file="):
			messageFile = strings.TrimPrefix(arg, "--message-file=")
		case arg == "--pane":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("notify.missing_pane_value")
			}
			pane, i = val, next
		case strings.HasPrefix(arg, "--pane="):
			pane = strings.TrimPrefix(arg, "--pane=")
		case arg == "--timeout":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("launch.missing_timeout_value")
			}
			n, err := strconv.ParseFloat(val, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(notifyError("notify.notify_timeout_must_be_a_finite_number_greater_than"))
			}
			timeout, i = n, next
		case strings.HasPrefix(arg, "--timeout="):
			n, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--timeout="), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(notifyError("notify.notify_timeout_must_be_a_finite_number_greater_than"))
			}
			timeout = n
		default:
			if strings.HasPrefix(arg, "-") {
				return usageFail("board.unknown_option", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return usageFail("launch.task_id_is_required")
	}
	root, err := board.BoardRoot()
	if err != nil {
		return fail(err)
	}
	if err := commandNotify(root, positional[0], message, messageFile, pane, messageSet, timeout); err != nil {
		return fail(err)
	}
	return 0
}
