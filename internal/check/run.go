package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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
				return command.UsageText(escapeText(parseErr.Message))
			}
			return command.Exit(runMode(mode, opt))
		},
	}
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
		return writeDelivery(result, code)
	default:
		result, code := runOverlap(ctx, git, opt.source, opt.head)
		return writeOverlap(result, code)
	}
}

// failure is the error object of an analysis that could not finish: the analyzer's own error
// code, in snake case, under a check_ prefix.
func failure(info *CheckError) error {
	code := "check_" + strings.ReplaceAll(info.Code, "-", "_")
	return command.Coded(code, errors.New(info.Message))
}

func writeDelivery(result DeliveryResult, code int) int {
	if command.JSON() {
		if result.Status == statusError {
			return command.FailStatus(code, failure(result.Error))
		}
		return command.ResultStatus(result, code)
	}
	text := humanDelivery(result)
	if result.Status == statusError {
		fmt.Fprint(os.Stderr, text)
		return code
	}
	fmt.Fprint(os.Stdout, text)
	return code
}

func writeOverlap(result OverlapResult, code int) int {
	if command.JSON() {
		if result.Status == statusError {
			return command.FailStatus(code, failure(result.Error))
		}
		return command.ResultStatus(result, code)
	}
	text := humanOverlap(result)
	if result.Status == statusError {
		fmt.Fprint(os.Stderr, text)
		return code
	}
	fmt.Fprint(os.Stdout, text)
	return code
}
