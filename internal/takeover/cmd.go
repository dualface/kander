package takeover

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/launch"
)

func init() {
	cli.Commands["dismiss"] = RunDismiss
	launch.CleanupTakeover = Cleanup
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kander: %s\n", err)
	return 1
}

func usage(w io.Writer) {
	fmt.Fprintln(w, t(
		"takeover.usage_kander_dismiss_timeout_seconds_task",
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

// RunDismiss implements kander dismiss.
func RunDismiss(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage(os.Stdout)
			return 0
		}
	}
	timeout := 120.0
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--timeout":
			val, next, ok := takeValue(args, i)
			if !ok {
				return usageFail("launch.missing_timeout_value")
			}
			n, err := strconv.ParseFloat(val, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(takeoverError("takeover.dismiss_timeout_must_be_a_finite_number_greater_than"))
			}
			timeout, i = n, next
		case strings.HasPrefix(arg, "--timeout="):
			n, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--timeout="), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fail(takeoverError("takeover.dismiss_timeout_must_be_a_finite_number_greater_than"))
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
	if err := commandDismiss(root, positional[0], timeout); err != nil {
		return fail(err)
	}
	return 0
}
