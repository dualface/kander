package check

import "github.com/dualface/kander/internal/cli"

func init() { cli.Register(NewCommand) }
