package server

import (
	"sync"
	"time"

	"github.com/dualface/kander/api"
)

// importProgressInterval spaces the file counts of one phase, and the repeat of
// the latest report while the import is busy with one slow step. A client that
// prints at least every 5 seconds therefore always has a report to show.
var importProgressInterval = time.Second

// importEmitter hands the progress of one import to its report function. A
// phase change is always reported; file counts are reported at most once per
// importProgressInterval, and the last count of a phase always is. While no
// report was made for a whole interval (one large file, a slow disk, a long
// check), a timer repeats the latest one. Reports are serialized, but may come
// from the importing goroutine or from the timer. The zero emitter and a nil
// one report nothing.
type importEmitter struct {
	report func(api.ImportProgress)

	mu      sync.Mutex
	last    time.Time
	current api.ImportProgress
	stopped chan struct{}
	done    chan struct{}
}

// newImportEmitter starts the timer when there is something to report to; stop
// ends it and must be called once the import is over.
func newImportEmitter(report func(api.ImportProgress)) *importEmitter {
	e := &importEmitter{report: report}
	if report != nil {
		e.stopped, e.done = make(chan struct{}), make(chan struct{})
		go e.repeat(importProgressInterval)
	}
	return e
}

func (e *importEmitter) repeat(interval time.Duration) {
	defer close(e.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopped:
			return
		case <-ticker.C:
			e.mu.Lock()
			if e.current.Phase != "" && time.Since(e.last) >= interval {
				e.report(e.current)
				e.last = time.Now()
			}
			e.mu.Unlock()
		}
	}
}

// stop ends the timer and waits for a report in progress to return.
func (e *importEmitter) stop() {
	if e == nil || e.report == nil {
		return
	}
	close(e.stopped)
	<-e.done
}

func (e *importEmitter) send(p api.ImportProgress) {
	if e == nil || e.report == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sendLocked(p)
}

func (e *importEmitter) sendLocked(p api.ImportProgress) {
	e.current, e.last = p, time.Now()
	e.report(p)
}

// phase reports the start of a phase without counts.
func (e *importEmitter) phase(name string) { e.send(api.ImportProgress{Phase: name}) }

// phaseCounts reports the start of a phase that already knows its totals.
func (e *importEmitter) phaseCounts(name string, done, total int, bytes, totalBytes int64) {
	e.send(api.ImportProgress{Phase: name, Done: done, Total: total, Bytes: bytes, TotalBytes: totalBytes})
}

// count reports file counts of the running phase. The throttle decides only
// whether to report now: the newest counts are always kept, so the timer never
// repeats older ones.
func (e *importEmitter) count(name string, done, total int, bytes, totalBytes int64) {
	if e == nil || e.report == nil {
		return
	}
	p := api.ImportProgress{Phase: name, Done: done, Total: total, Bytes: bytes, TotalBytes: totalBytes}
	e.mu.Lock()
	defer e.mu.Unlock()
	if done < total && time.Since(e.last) < importProgressInterval {
		e.current = p
		return
	}
	e.sendLocked(p)
}

// counter adapts count to the callback of scanTree.
func (e *importEmitter) counter(name string, totalBytes int64) func(done, total int, bytes int64) {
	if e == nil || e.report == nil {
		return nil
	}
	return func(done, total int, bytes int64) { e.count(name, done, total, bytes, totalBytes) }
}
