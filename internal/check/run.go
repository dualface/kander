package check

import (
	"context"
	"fmt"
	"os"

	"github.com/dualface/kander/internal/config"
)

// Run is the kander check command for the board-free Git analyzers; the caller
// has already matched args[0] against checkDelivery or checkOverlap. Every other
// form of check goes through the server.
func Run(args []string) int { return runMode(args[0], args[1:]) }

func runMode(mode string, args []string) int {
	opt, parseErr := parseOptions(mode, args)
	if opt.help && parseErr == nil {
		fmt.Fprintln(os.Stdout, config.CommandUsage("check"))
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
	fmt.Fprintln(os.Stderr, config.CommandUsage("check"))
	fmt.Fprintln(os.Stderr, "kander: "+escapeText(errInfo.Message))
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
