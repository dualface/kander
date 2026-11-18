package server

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/command"
)

// projectList is the --json output of `kander server projects`.
type projectList struct {
	Projects []api.Project `json:"projects"`
}

// NewProjectsCommand builds `kander server projects`: a read-only listing of the registry. It
// takes no lock and writes nothing, so it also runs beside a serving instance; projects.json is
// replaced atomically.
func NewProjectsCommand() *cobra.Command { return newProjectsCommand(os.Stdout, os.Stderr) }

func newProjectsCommand(stdout, stderr io.Writer) *cobra.Command {
	var data string
	var jsonOut bool
	c := &cobra.Command{
		Use:  "projects",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error {
			return command.Exit(listProjects(data, jsonOut, stdout, stderr))
		},
	}
	c.Flags().StringVar(&data, "data", "", "")
	c.Flags().BoolVar(&jsonOut, "json", false, "")
	return c
}

func listProjects(data string, jsonOut bool, stdout, stderr io.Writer) int {
	dir, err := dataDir(data)
	if err != nil {
		fmt.Fprintln(stderr, "kander server:", err)
		return 1
	}
	var projects []api.Project
	if _, err := os.Lstat(dir); err == nil {
		if projects, err = readRegistry(dir); err != nil {
			fmt.Fprintln(stderr, "kander server:", err)
			return 1
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "kander server:", err)
		return 1
	}
	if jsonOut {
		if projects == nil {
			projects = []api.Project{}
		}
		out, _ := json.MarshalIndent(projectList{Projects: projects}, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	for _, p := range projects {
		fmt.Fprintf(stdout, "%s\n  id:      %s\n  remote:  %s\n  created: %s\n  board:   %s\n", p.Name, p.ID, cmp.Or(p.Remote, "-"), p.CreatedAt.Format(time.RFC3339), p.BoardPath)
	}
	return 0
}
