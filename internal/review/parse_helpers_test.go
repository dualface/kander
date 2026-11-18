package review

import (
	"errors"

	"github.com/dualface/kander/internal/command/commandtest"
)

// parseArchiveOptions runs the argument handling of the review command on args and returns the
// archive options and the positional arguments it would hand to the review.
func parseArchiveOptions(args []string) (archiveOptions, []string, error) {
	var options archiveOptions
	var positional []string
	run := func(parsed archiveOptions, rest []string) int {
		options, positional = parsed, rest
		return 0
	}
	code, _, reported := commandtest.Capture(func() int { return commandtest.Run(newCommand(run), args...) })
	if code != 0 {
		return options, nil, errors.New(reported)
	}
	return options, positional, nil
}
