package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
)

// AggregateReview publishes the disposition view of batch and returns the
// server's answer.
//
// The server binds only the batch ID, so the view is never part of the joint
// material. The replay record is keyed by the hash of the view read first
// instead: a batch whose view changed since its last aggregation is published
// again rather than replaying the saved result of the earlier publication. A
// fresh publication may return a newer view than the one read, because the
// server builds it inside its own transaction; a reused publication must
// return exactly that view, or the answer is refused as stale.
func (s *Session) AggregateReview(ctx context.Context, batch string) (json.RawMessage, error) {
	raw, err := s.Project.ReviewBatchView(ctx, batch)
	if err != nil {
		return nil, err
	}
	var current board.ReviewBatchView
	if err := json.Unmarshal(raw, &current); err != nil {
		return nil, err
	}
	viewHash := board.ReviewViewDigest(current)
	operation := "review-aggregate:" + batch
	proof, reused, err := s.reviewAuthority(ctx, operation, operation+"@view:"+viewHash, batch, current.Batch.TaskIDs, "")
	if err != nil {
		return nil, err
	}
	out, err := s.Project.ReviewAggregate(ctx, client.NewKey(), batch, proof)
	if err != nil || !reused {
		return out, err
	}
	var saved board.ReviewBatchView
	if err := json.Unmarshal(out, &saved); err != nil {
		return nil, err
	}
	if board.ReviewViewDigest(saved) != viewHash {
		return nil, errors.New(config.Text("remote.review_aggregate_stale", batch))
	}
	return out, nil
}
