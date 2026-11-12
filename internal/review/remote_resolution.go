package review

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

func remoteResolveBatch(ctx context.Context, session *remote.Session, cwd string, extension board.ReviewPlanExtension) (json.RawMessage, error) {
	raw, err := session.Project.ReviewAction(ctx, client.NewKey(), "resolution-facts", extension, nil)
	if err != nil {
		return nil, err
	}
	var facts board.ReviewResolutionFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		return nil, err
	}
	for _, edge := range facts.Edges {
		if session.Info.Remote != "" {
			for _, commit := range []string{edge.Ancestor, edge.Descendant} {
				if err := session.FetchCommit(ctx, cwd, commit); err != nil {
					return nil, err
				}
			}
		}
		if err := verifyGitEdge(cwd, edge); err != nil {
			return nil, err
		}
	}
	payload := api.ReviewResolution{Extension: extension, Facts: facts, Git: board.ReviewResolutionGit{CWD: facts.CWD, Edges: facts.Edges, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	proof, err := session.ReviewAuthority(ctx, "review-resolve-batch", payload, facts.TaskIDs)
	if err != nil {
		return nil, err
	}
	return session.Project.ReviewAction(ctx, client.NewKey(), "resolve-batch", payload, proof)
}
