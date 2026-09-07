package liveness

import (
	"context"
	"sync"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/probe"
)

const (
	DefaultBatchBudget      = 10 * time.Second
	DefaultBatchConcurrency = 4
)

// TaskInput owns the immutable card snapshot used for one observation.
// ReadError preserves a failed document read without probing incomplete metadata.
type TaskInput struct {
	Entry     board.Entry
	Text      string
	ReadError error
}

// BatchOptions bounds the entire collection, including queue time and cleanup.
// Non-positive values select the defaults. An earlier caller deadline wins.
type BatchOptions struct {
	Budget      time.Duration
	Concurrency int
}

// ClassifyTasksContext returns one report per input, in input order. A fixed
// worker set owns every probe and is joined before returning; no work survives
// the call. OS cleanup can exceed the deadline as documented for CaptureContext.
func ClassifyTasksContext(ctx context.Context, tasks []TaskInput, options BatchOptions) []Report {
	if options.Budget <= 0 {
		options.Budget = DefaultBatchBudget
	}
	if options.Concurrency <= 0 {
		options.Concurrency = DefaultBatchConcurrency
	}
	ctx, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()
	inputs := append([]TaskInput(nil), tasks...)
	reports := make([]Report, len(inputs))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for n := 0; n < min(options.Concurrency, len(inputs)); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				input := inputs[i]
				if ctx.Err() != nil {
					reports[i] = uncollectedReport(input, ctx.Err())
				} else if input.ReadError != nil {
					reports[i] = uncollectedReport(input, input.ReadError)
				} else {
					reports[i] = ClassifyTaskContext(ctx, input.Entry, input.Text)
				}
			}
		}()
	}
	for i, input := range inputs {
		select {
		case <-ctx.Done():
			reports[i] = uncollectedReport(input, ctx.Err())
		case jobs <- i:
		}
	}
	close(jobs)
	workers.Wait()
	return reports
}

func uncollectedReport(input TaskInput, err error) Report {
	out := unknownFrom(input.Entry, input.Text, t("liveness.batch_not_observed", probe.FailureDetail(err)))
	out.Identity = identityFrom(input.Entry, input.Text)
	return out
}
