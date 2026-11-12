package review

import "github.com/dualface/kander/internal/cli"

func init() {
	cli.Commands["review"] = Run
	cli.RemoteCommands["review"] = func(args []string) int {
		for _, arg := range args {
			if arg == "--schema" {
				return Run(args)
			}
		}
		return cli.RequireService(RunRemote)(args)
	}
}
