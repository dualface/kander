package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/cli"
)

func TestExternalEmbedderBuildsWithTheBinaryCommandTree(t *testing.T) {
	name := "embedder"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-mod=readonly", "-o", binary, "./...")
	build.Dir = filepath.Join("..", "..", "testdata", "embedder")
	build.Env = append(build.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build independent embedder: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(t.Context(), binary).Output()
	if err != nil {
		t.Fatal(err)
	}
	embedded := strings.Split(strings.TrimSpace(string(out)), "\n")
	slices.Sort(embedded)

	root := cli.NewRoot()
	root.InitDefaultHelpCmd()
	var binaryCommands []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			binaryCommands = append(binaryCommands, sub.CommandPath())
			walk(sub)
		}
	}
	walk(root)
	slices.Sort(binaryCommands)
	if !slices.Equal(embedded, binaryCommands) {
		t.Fatalf("embedder commands = %q\nKander commands = %q", embedded, binaryCommands)
	}
}
