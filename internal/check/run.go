package check

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/liveness"
)

// NewCommand builds kander check. The Git analyzers delivery and overlap run locally and
// never touch the board; every other form of check goes through the server.
func NewCommand() *cobra.Command {
	var all bool
	c := command.RequireProject(&cobra.Command{
		Use:  "check [task-id...]",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, tasks []string) error { return command.Exit(liveness.RunCheckRemote(tasks, all)) },
	})
	c.Flags().BoolVar(&all, "all", false, "")
	c.AddCommand(newModeCommand(checkDelivery), newModeCommand(checkOverlap))
	return c
}

// newModeCommand builds one Git analyzer: delivery takes --base and --commit, overlap takes
// --source and --head.
func newModeCommand(mode string) *cobra.Command {
	opt := options{commit: "HEAD", head: "HEAD"}
	c := &cobra.Command{
		Use:  mode,
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error {
			if parseErr := opt.validate(mode); parseErr != nil {
				if !opt.json {
					return command.UsageText(escapeText(parseErr.Message))
				}
				return command.Exit(writeModeError(mode, parseErr, exitUsage))
			}
			return command.Exit(runMode(mode, opt))
		},
	}
	c.Flags().BoolVar(&opt.json, "json", false, "")
	if mode == checkDelivery {
		command.Once(c.Flags(), &opt.base, "base", "")
		command.Once(c.Flags(), &opt.commit, "commit", "")
	} else {
		command.Once(c.Flags(), &opt.source, "source", "")
		command.Once(c.Flags(), &opt.head, "head", "")
	}
	return c
}

func runMode(mode string, opt options) int {
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

// writeModeError reports a usage error found after the flags were parsed as the JSON result
// the caller asked for.
func writeModeError(mode string, errInfo *CheckError, code int) int {
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
