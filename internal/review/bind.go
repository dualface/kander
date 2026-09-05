package review

import "github.com/dualface/kander/internal/cli"

func init() {
	cli.Commands["review"] = Run
}
