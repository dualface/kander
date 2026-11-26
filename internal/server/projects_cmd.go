package server

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command"
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
	c := &cobra.Command{
		Use:  "projects",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error {
			return command.Exit(listProjects(data, stdout, stderr))
		},
	}
	c.Flags().StringVar(&data, "data", "", "")
	return c
}

// failProjects reports a failure of kander server projects: the error object with --json,
// otherwise the text on stderr.
func failProjects(stderr io.Writer, err error) int {
	if command.JSON() {
		return command.Fail(err)
	}
	fmt.Fprintln(stderr, "kander server:", err)
	return 1
}

func listProjects(data string, stdout, stderr io.Writer) int {
	dir, err := dataDir(data)
	if err != nil {
		return failProjects(stderr, err)
	}
	var projects []api.Project
	if _, err := os.Lstat(dir); err == nil {
		if projects, err = readRegistry(dir); err != nil {
			return failProjects(stderr, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return failProjects(stderr, err)
	}
	if command.JSON() {
		if projects == nil {
			projects = []api.Project{}
		}
		return command.Result(projectList{Projects: projects})
	}
	for _, p := range projects {
		fmt.Fprintf(stdout, "%s\n  id:      %s\n  remote:  %s\n  created: %s\n  board:   %s\n", p.Name, p.ID, cmp.Or(p.Remote, "-"), p.CreatedAt.Format(time.RFC3339), p.BoardPath)
	}
	return 0
}
