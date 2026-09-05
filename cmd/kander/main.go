package main

import (
	"os"

	"github.com/dualface/kander/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
