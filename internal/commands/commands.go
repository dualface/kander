// Package commands wires the CLI's implementation packages into one tree.
package commands

import (
	_ "github.com/dualface/kander/v2/internal/check"
	_ "github.com/dualface/kander/v2/internal/launch"
	_ "github.com/dualface/kander/v2/internal/liveness"
	_ "github.com/dualface/kander/v2/internal/notify"
	_ "github.com/dualface/kander/v2/internal/review"
	_ "github.com/dualface/kander/v2/internal/takeover"
	_ "github.com/dualface/kander/v2/internal/tui"
)
