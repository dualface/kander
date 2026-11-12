package client

import (
	"context"
	"net/http"

	"github.com/dualface/kander/internal/board"
)

func (p *Project) ExecutionRecord(ctx context.Context, task string) (board.ExecutionRecord, error) {
	var record board.ExecutionRecord
	err := p.c.do(ctx, call{method: http.MethodGet, path: p.path(taskPath(task, "execution", "record")...)}, &record)
	return record, err
}
