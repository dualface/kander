package server

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dualface/kander/internal/api"
)

// projectList is the --json output of `kander-server projects`.
type projectList struct {
	Projects []api.Project `json:"projects"`
}

// runProjects is `kander-server projects`: a read-only listing of the
// registry. It takes no lock and writes nothing, so it also runs beside a
// serving instance; projects.json is replaced atomically.
func runProjects(args []string) int {
	return listProjects(args, os.Stdout, os.Stderr)
}

func listProjects(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("kander-server projects", flag.ContinueOnError)
	flags.SetOutput(stderr)
	data := flags.String("data", "", "data directory")
	jsonOut := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "usage: kander-server projects [--data DIR] [--json]")
		return 2
	}
	dir, err := dataDir(*data)
	if err != nil {
		fmt.Fprintln(stderr, "kander-server:", err)
		return 1
	}
	var projects []api.Project
	if _, err := os.Lstat(dir); err == nil {
		if projects, err = readRegistry(dir); err != nil {
			fmt.Fprintln(stderr, "kander-server:", err)
			return 1
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "kander-server:", err)
		return 1
	}
	if *jsonOut {
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
