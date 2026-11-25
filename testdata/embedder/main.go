// Command embedder proves the public Kander layer builds in a separate module.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/cli"
	"github.com/dualface/kander/client"
	"github.com/dualface/kander/server"
)

var (
	_ func(string, string) (*client.Client, error)                   = client.New
	_ func(*client.Client, context.Context) (api.VersionInfo, error) = (*client.Client).Version
	_ func(server.Options) (*server.Server, error)                   = server.Open
	_ interface {
		Handler() http.Handler
		Subscribe(context.Context, string, string) (<-chan server.Notification, error)
		Close() error
	} = (*server.Server)(nil)
)

func main() {
	root, err := cli.NewRoot(cli.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printCommands(root)
}

func printCommands(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		fmt.Println(sub.CommandPath())
		printCommands(sub)
	}
}
