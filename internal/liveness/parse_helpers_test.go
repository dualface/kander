package liveness

import "github.com/dualface/kander/v2/internal/command/commandtest"

// parseSubscribeArgs runs the argument handling of kander subscribe on args and returns the
// subscription it would start, or the report of the argument error.
func parseSubscribeArgs(args []string) (subscribeOptions, string) {
	var parsed subscribeOptions
	run := func(opts subscribeOptions) int {
		parsed = opts
		return 0
	}
	code, _, reported := commandtest.Capture(func() int { return commandtest.Run(newSubscribeCommand(run), args...) })
	if code != 0 {
		return parsed, reported
	}
	return parsed, ""
}
