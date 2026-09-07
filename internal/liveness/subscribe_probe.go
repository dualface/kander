package liveness

import (
	"context"
	"sync"
	"time"
)

type subscriptionBatch struct {
	revisions map[string]uint64
	reports   []Report
}

type subscriptionProbes struct {
	ctx     context.Context
	cancel  context.CancelFunc
	results chan subscriptionBatch
	running bool
	workers sync.WaitGroup
	cached  map[string]subscriptionObservation
}

type subscriptionObservation struct {
	revision uint64
	report   Report
}

func newSubscriptionProbes(ctx context.Context) *subscriptionProbes {
	ctx, cancel := context.WithCancel(ctx)
	return &subscriptionProbes{ctx: ctx, cancel: cancel, results: make(chan subscriptionBatch, 1), cached: map[string]subscriptionObservation{}}
}

func (p *subscriptionProbes) start(facts subscriptionFacts) {
	if p.running || p.ctx.Err() != nil {
		return
	}
	var inputs []TaskInput
	revisions := map[string]uint64{}
	for _, id := range facts.monitored {
		if !facts.needsProbe(id) {
			continue
		}
		text, err := facts.scanned.Document(id)
		inputs = append(inputs, TaskInput{Entry: facts.scanned.Entries[id], Text: text, ReadError: err})
		revisions[id] = facts.revisions[id]
	}
	if len(inputs) == 0 {
		return
	}
	p.running = true
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		reports := ClassifyTasksContext(p.ctx, inputs, BatchOptions{})
		p.results <- subscriptionBatch{revisions: revisions, reports: reports}
	}()
}

func (p *subscriptionProbes) accept(batch subscriptionBatch) {
	p.workers.Wait()
	p.running = false
	p.cached = make(map[string]subscriptionObservation, len(batch.reports))
	for _, report := range batch.reports {
		p.cached[report.TaskID] = subscriptionObservation{revision: batch.revisions[report.TaskID], report: report}
	}
}

// poll joins only a batch whose result is already available.
func (p *subscriptionProbes) poll() bool {
	if p.running {
		select {
		case batch := <-p.results:
			p.accept(batch)
			return true
		default:
		}
	}
	return false
}

func (p *subscriptionProbes) finish() {
	p.cancel()
	if p.running {
		p.accept(<-p.results)
	}
}

func (p *subscriptionProbes) liveness(facts subscriptionFacts, heartbeat time.Duration) map[string]livenessJSON {
	// Never wait for a probe when formatting an event.
	p.poll()
	var out map[string]livenessJSON
	for _, id := range facts.monitored {
		if !facts.needsProbe(id) {
			delete(p.cached, id)
			continue
		}
		if out == nil {
			out = map[string]livenessJSON{}
		}
		text, _ := facts.scanned.Document(id)
		identity := identityFrom(facts.scanned.Entries[id], text)
		agent := "N/A"
		if session := ParseTaskSession(text); session != nil {
			agent = session.Agent
		}
		value := livenessJSON{Agent: agent, Status: Unknown, Channel: "unknown", Detail: t("liveness.subscription_observation_pending"), RuntimeState: "unknown", Revision: facts.revisions[id], CollectionState: "pending", Identity: identity}
		if cached, ok := p.cached[id]; ok && cached.revision == facts.revisions[id] && cached.report.Identity == identity {
			report := cached.report
			value.Agent, value.Status, value.Channel, value.Detail = report.Agent, report.Status, report.Channel, report.Detail
			value.RuntimeState, value.ObservationValid = report.RuntimeState, report.ObservationValid
			value.NewWindow = report.NewWindow
			if !report.ObservedAt.IsZero() {
				observed := report.ObservedAt
				value.ObservedAt = &observed
				age := time.Since(observed)
				value.AgeSeconds = max(0, age.Seconds())
				value.CollectionState = "complete"
				if age > heartbeat && age-heartbeat > DefaultBatchBudget {
					value.Stale = true
					value.Status = Unknown
					value.ObservationValid = false
					value.RuntimeState = "unknown"
					value.Detail = t("liveness.subscription_observation_stale")
				}
			} else {
				value.CollectionState = "not-observed"
			}
		} else {
			delete(p.cached, id)
		}
		value.Collecting = p.running
		out[id] = value
	}
	return out
}
