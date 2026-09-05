package menu

import "os"

func stdinStderrTTY() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stderr)
}
