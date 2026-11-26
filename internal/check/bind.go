package check

import "github.com/dualface/kander/v2/internal/cli"

func init() { cli.Register(NewCommand) }
