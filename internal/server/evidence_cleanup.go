package server

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

// evidenceCleanupRoutes serve the read-only cleanup scope of the Git evidence
// of a done card and of a finished task group. The server runs no Git; the
// client deletes the refs.
func (s *Server) evidenceCleanupRoutes(mux *http.ServeMux) {
	s.projectRoute(mux, "GET /api/v1/projects/{project}/tasks/{task}/evidence-cleanup", func(p *project, r *http.Request) outcome {
		value, err := p.taskEvidenceCleanup(r.Context(), r.PathValue("task"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "GET /api/v1/projects/{project}/groups/{group}/evidence-cleanup", func(p *project, r *http.Request) outcome {
		group := r.PathValue("group")
		if !board.ValidTaskGroup(group) {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid task group identity")}
		}
		value, err := p.groupEvidenceCleanup(r.Context(), group)
		return result(http.StatusOK, value, err)
	})
}

// taskEvidenceCleanup answers for a card. A card of a task group answers with
// its group, whose evidence goes only once the whole group is finished.
func (p *project) taskEvidenceCleanup(ctx context.Context, task string) (api.EvidenceCleanup, error) {
	id, err := board.NormalizeTaskID(task)
	if err != nil {
		return api.EvidenceCleanup{}, err
	}
	s, err := board.ReadSnapshot(p.board, id)
	if err != nil {
		return api.EvidenceCleanup{}, err
	}
	if group := board.TaskGroupFrom(s.Text); group != "" {
		return p.groupEvidenceCleanup(ctx, group)
	}
	if s.Entry.State != "done" {
		return api.EvidenceCleanup{}, nil
	}
	// A standalone integration names no task: its only member is the card.
	sources, err := p.integrationSources(func(request api.IntegrationRequest) bool {
		return request.GroupID == "" && len(request.Members) == 1 && request.Members[0].TaskID == id
	})
	if err != nil {
		return api.EvidenceCleanup{}, err
	}
	return api.EvidenceCleanup{Ready: true, Tasks: []string{id}, Sources: sources}, nil
}

func (p *project) groupEvidenceCleanup(ctx context.Context, group string) (api.EvidenceCleanup, error) {
	settlement, err := board.GroupEvidenceSettled(ctx, p.board, group)
	if err != nil || !settlement.Settled {
		return api.EvidenceCleanup{}, err
	}
	sources, err := p.integrationSources(func(request api.IntegrationRequest) bool { return request.GroupID == group })
	if err != nil {
		return api.EvidenceCleanup{}, err
	}
	return api.EvidenceCleanup{Ready: true, Tasks: settlement.Done, Sources: sources}, nil
}

// integrationSources scans every recorded integration execution of every
// target: an operation rotates its attempt when the target moves or its claim
// ends, and each attempt may have published its own source ref.
func (p *project) integrationSources(match func(api.IntegrationRequest) bool) ([]api.EvidenceSource, error) {
	targets, err := p.serviceNames("integrations", "", fs.KindDirectory)
	if err != nil {
		return nil, err
	}
	slices.Sort(targets)
	var sources []api.EvidenceSource
	for _, target := range targets {
		if !strings.HasPrefix(target, "i-") || !api.ValidIdentity(target) {
			continue
		}
		executions, err := p.serviceNames(filepath.Join("integrations", target, "executions"), ".json", fs.KindFile)
		if err != nil {
			return nil, err
		}
		slices.Sort(executions)
		for _, execution := range executions {
			if !api.ValidIdentity(execution) {
				continue
			}
			var value api.Integration
			found, err := readJSON(p.root, servicePath("integrations", target, "executions", execution+".json"), &value)
			if err != nil {
				return nil, err
			}
			if !found || value.Request.ExecutionID != execution || !api.CommitSHA(value.Request.Commit) || !match(value.Request) {
				continue
			}
			sources = append(sources, api.EvidenceSource{ExecutionID: execution, Commit: value.Request.Commit})
		}
	}
	return sources, nil
}
