package main

import (
	"os"

	"github.com/dualface/kander/internal/server"
)

func main() {
	os.Exit(server.Main(os.Args[1:]))
}
