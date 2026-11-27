package remote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
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
	session, err := OpenWith(ctx, cfg, dir, OpenOptions{QuietPolicy: true})
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
	if command.JSON() {
		initPolicyStep(ctx, session, cfg)
		return command.Exit(command.Result(bindResult{ProjectName: session.Info.Name, ProjectID: session.Info.ID, ServerURL: session.ServerURL, BoardPath: session.Info.BoardPath}))
	}
	fmt.Fprintln(stdout, config.Text("remote.project_bound", session.Info.Name, session.Info.ID, session.ServerURL))
	if session.Info.BoardPath != "" {
		fmt.Fprintln(stdout, config.Text("remote.project_board_path", session.Info.BoardPath))
	}
	initPolicyStep(ctx, session, cfg)
	return nil
}

// initPolicyStep offers the policy initialization of kander policy init when
// the bound project has no policy: a person at a terminal sees the values and
// confirms; any other caller gets the project_policy_unset remark and nothing
// is written. A failed initialization is a remark: the binding stands.
func initPolicyStep(ctx context.Context, session *Session, cfg *config.Config) {
	policy := session.Info.Policy
	if policy != nil && policy.Revision > 0 {
		return
	}
	if policy == nil || !policyInteractive() {
		remark(config.NoticePolicyUnset, config.Text("policy.unset"))
		return
	}
	written, err := initializePolicy(ctx, session, config.LocalPolicyValues(cfg), true)
	switch {
	case err != nil:
		remark("project_policy_init_failed", config.Text("policy.init_failed", err.Error()))
	case written != nil:
		fmt.Fprintln(stdout, config.Text("policy.written", written.Revision, written.Epoch))
	}
}
