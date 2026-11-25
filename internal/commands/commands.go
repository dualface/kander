// Package commands wires the CLI's implementation packages into one tree.
package commands

import (
	_ "github.com/dualface/kander/internal/check"
	_ "github.com/dualface/kander/internal/launch"
	_ "github.com/dualface/kander/internal/liveness"
	_ "github.com/dualface/kander/internal/notify"
	_ "github.com/dualface/kander/internal/review"
	_ "github.com/dualface/kander/internal/takeover"
	_ "github.com/dualface/kander/internal/tui"
)
