package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/version"
)

type maintenanceRequest struct {
	Project      string
	Acknowledged bool
}

// MaintenanceResult reports one explicit offline form migration. The board
// journal retains its exact before/after images; import originals stay intact.
type MaintenanceResult struct {
	Project      api.Project `json:"project"`
	ChangedTasks int         `json:"changed_tasks"`
	Snapshot     string      `json:"snapshot"`
}

// Maintain holds the server's exclusive data-directory lock while the board's
// existing migration producer recovers and converts legacy file cards. It
// never serves requests, prunes archived evidence or changes the import source.
func Maintain(options Options, project string, acknowledged bool) (result MaintenanceResult, err error) {
	if !api.ValidProjectName(project) {
		return result, apiError(api.CodeInvalidRequest, "invalid maintenance project name: "+api.ProjectNameRule)
	}
	options.maintenance = &maintenanceRequest{Project: project, Acknowledged: acknowledged}
	service, err := Open(options)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, service.Close()) }()
	info, err := service.projectByName(project)
	if err != nil {
		return result, err
	}
	p, err := service.project(info.ID)
	if err != nil {
		return result, err
	}
	result = MaintenanceResult{Project: info, ChangedTasks: service.maintenanceChanged}
	code, _, problems, checkErr := board.CheckBoardForImport(p.board)
	if checkErr != nil {
		return result, checkErr
	}
	if code != 0 {
		return result, fmt.Errorf("maintained board requires repair: %s", strings.Join(problems, "; "))
	}
	_, digest, err := snapshotTree(p.board)
	if err != nil {
		return result, err
	}
	result = MaintenanceResult{Project: info, ChangedTasks: service.maintenanceChanged, Snapshot: digest}
	return result, nil
}

// NewMaintainCommand builds `kander server maintain`, the offline form migration of one project.
func NewMaintainCommand() *cobra.Command {
	var data, project string
	var maintenance bool
	c := &cobra.Command{
		Use:  "maintain",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runMaintain(data, project, maintenance)) },
	}
	c.PreRunE = func(*cobra.Command, []string) error { return command.Required(c, "project") }
	c.Flags().StringVar(&data, "data", "", "")
	c.Flags().StringVar(&project, "project", "", "")
	c.Flags().BoolVar(&maintenance, "maintenance", false, "")
	return c
}

func runMaintain(data, project string, maintenance bool) int {
	dir, err := dataDir(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	result, err := Maintain(Options{DataDir: dir, Logger: logger, Version: version.String()}, project, maintenance)
	if err != nil {
		logger.Error("offline maintenance failed", "project_name", project, "changed_tasks", result.ChangedTasks, "error", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
