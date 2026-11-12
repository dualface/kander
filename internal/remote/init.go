package remote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// RunInit binds a checkout to a service project. It never creates a local board.
func RunInit(args []string) int {
	args, url, urlGiven, err := takeValue(args, "--server")
	if err != nil {
		return printUsage("init")
	}
	args, name, nameGiven, err := takeValue(args, "--project")
	if err != nil {
		return printUsage("init")
	}
	args, maintenance := takeFlag(args, "--maintenance")
	if len(args) > 1 || len(args) == 1 && strings.HasPrefix(args[0], "-") {
		return printUsage("init")
	}
	if maintenance {
		return fail(errors.New(config.Text("remote.init_maintenance_server")))
	}
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
	return 0
}
