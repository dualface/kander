package server

import (
	"time"

	"github.com/dualface/kander/internal/api"
)

// importProgressInterval spaces the file counts of one phase, so a client that
// prints at least every 5 seconds always has a fresh count to show.
const importProgressInterval = time.Second

// importEmitter hands the progress of one import to its report function. A
// phase change is always reported; file counts are reported at most once per
// importProgressInterval, and the last count of a phase always is. The zero
// emitter and a nil one report nothing.
type importEmitter struct {
	report func(api.ImportProgress)
	last   time.Time
}

func (e *importEmitter) send(p api.ImportProgress) {
	if e == nil || e.report == nil {
		return
	}
	e.last = time.Now()
	e.report(p)
}

// phase reports the start of a phase without counts.
func (e *importEmitter) phase(name string) { e.send(api.ImportProgress{Phase: name}) }

// phaseCounts reports the start of a phase that already knows its totals.
func (e *importEmitter) phaseCounts(name string, done, total int, bytes, totalBytes int64) {
	e.send(api.ImportProgress{Phase: name, Done: done, Total: total, Bytes: bytes, TotalBytes: totalBytes})
}

// count reports file counts of the running phase, throttled.
func (e *importEmitter) count(name string, done, total int, bytes, totalBytes int64) {
	if e == nil || e.report == nil {
		return
	}
	if done < total && time.Since(e.last) < importProgressInterval {
		return
	}
	e.phaseCounts(name, done, total, bytes, totalBytes)
}

// counter adapts count to the callback of scanTree.
func (e *importEmitter) counter(name string, totalBytes int64) func(done, total int, bytes int64) {
	if e == nil || e.report == nil {
		return nil
	}
	return func(done, total int, bytes int64) { e.count(name, done, total, bytes, totalBytes) }
}
