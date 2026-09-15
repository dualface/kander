package check

import (
	"context"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/liveness"
)

// Run is the kander check command. delivery and overlap are board-free Git
// analyzers; every other form delegates to the existing liveness runner.
func Run(args []string) int {
	if len(args) == 0 {
		return liveness.RunCheck(args)
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stdout, t("check.usage"))
		return 0
	}
	switch args[0] {
	case checkDelivery, checkOverlap:
		return runMode(args[0], args[1:])
	default:
		return liveness.RunCheck(args)
	}
}

func runMode(mode string, args []string) int {
	opt, parseErr := parseOptions(mode, args)
	if opt.help && parseErr == nil {
		fmt.Fprintln(os.Stdout, usageFor(mode))
		return 0
	}
	if parseErr != nil {
		return writeModeError(mode, opt.json, parseErr, exitUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitOverallLimit)
	defer cancel()
	git := newGitRunner(nil)
	switch mode {
	case checkDelivery:
		result, code := runDelivery(ctx, git, opt.base, opt.commit)
		return writeDelivery(opt.json, result, code)
	default:
		result, code := runOverlap(ctx, git, opt.source, opt.head)
		return writeOverlap(opt.json, result, code)
	}
}

func writeModeError(mode string, jsonMode bool, errInfo *CheckError, code int) int {
	if jsonMode {
		var payload any
		if mode == checkDelivery {
			payload = deliveryError(errInfo.Code, errInfo.Message)
		} else {
			payload = overlapError(errInfo.Code, errInfo.Message)
		}
		if writeErr := writeJSON(payload); writeErr != nil {
			fmt.Fprintln(os.Stderr, "kander: "+t("check.internal"))
			return exitExec
		}
		return code
	}
	fmt.Fprintln(os.Stderr, usageFor(mode))
	fmt.Fprintln(os.Stderr, "kander: "+errInfo.Message)
	return code
}

func writeDelivery(jsonMode bool, result DeliveryResult, code int) int {
	if jsonMode {
		if writeErr := writeJSON(result); writeErr != nil {
			return writeJSONOrFallback(deliveryError(errCodeInternal, t("check.internal")), checkDelivery)
		}
		return code
	}
	text := humanDelivery(result)
	if result.Status == statusError {
		fmt.Fprint(os.Stderr, text)
		return code
	}
	fmt.Fprint(os.Stdout, text)
	return code
}

func writeOverlap(jsonMode bool, result OverlapResult, code int) int {
	if jsonMode {
		if writeErr := writeJSON(result); writeErr != nil {
			return writeJSONOrFallback(overlapError(errCodeInternal, t("check.internal")), checkOverlap)
		}
		return code
	}
	text := humanOverlap(result)
	if result.Status == statusError {
		fmt.Fprint(os.Stderr, text)
		return code
	}
	fmt.Fprint(os.Stdout, text)
	return code
}
