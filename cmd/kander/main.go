package main

import (
	"os"

	"github.com/dualface/kander/v2/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
