package remote

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
)

// NewInitCommand builds kander init, which binds a checkout to a service project. It never
// creates a local board.
func NewInitCommand() *cobra.Command {
	var url, name string
	c := &cobra.Command{
		Use:  "init [project-path]",
		Args: command.Args(0, 1),
	}
	c.RunE = func(_ *cobra.Command, args []string) error {
		return runInit(args, url, c.Flags().Changed("server"), name, c.Flags().Changed("project"))
	}
	c.Flags().StringVar(&url, "server", "", "")
	c.Flags().StringVar(&name, "project", "", "")
	return c
}

func runInit(args []string, url string, urlGiven bool, name string, nameGiven bool) error {
	dir, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	if len(args) == 1 {
		dir, err = filepath.Abs(args[0])
		if err != nil {
			return fail(err)
		}
	}
	location, err := config.ResolveOverlayLocation(dir)
	if err != nil {
		return fail(err)
	}
	baseline, err := config.ReadOverlayFile(location.Path)
	if err != nil {
		return fail(err)
	}
	overlay := config.CloneOverlay(baseline)
	if urlGiven {
		config.OverlaySet(overlay, url, "server", "url")
	}
	if nameGiven {
		config.OverlaySet(overlay, name, "project", "name")
	}
	raw, err := config.LoadScopeDocument(false)
	if err != nil {
		return fail(err)
	}
	cfg, err := config.MergeOverlayOnRaw(raw, overlay)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := commandContext()
	defer cancel()
	session, err := Open(ctx, cfg, dir)
	if err != nil {
		return fail(err)
	}
	// Registration is idempotent by project identity. A concurrent configuration
	// edit must not be overwritten even when registration has already succeeded.
	if urlGiven || nameGiven {
		if _, err := config.SaveOverlayIfUnchanged(location.Path, overlay, baseline); err != nil {
			return fail(err)
		}
	}
	fmt.Fprintln(stdout, config.Text("remote.project_bound", session.Info.Name, session.Info.ID, session.ServerURL))
	if session.Info.BoardPath != "" {
		fmt.Fprintln(stdout, config.Text("remote.project_board_path", session.Info.BoardPath))
	}
	return nil
}
