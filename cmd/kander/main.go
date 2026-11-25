package main

import (
	"os"

	"github.com/dualface/kander/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
