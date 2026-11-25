package server

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/config"
)

// importLineInterval spaces the counts of one phase on a stderr that is not a
// terminal; the server reports them at least every second.
const importLineInterval = 2 * time.Second

// importReporter prints the progress of `kander server import` on stderr. A
// terminal shows one status line that is rewritten in place; anything else
// gets one plain line per phase and a count line every importLineInterval,
// with no control characters.
type importReporter struct {
	mu       sync.Mutex
	w        io.Writer
	tty      bool
	width    int  // display width of the status line on a terminal
	open     bool // the terminal status line has no newline yet
	phase    string
	seen     bool // any report arrived
	pastCopy bool
	printed  time.Time
}

func newImportReporter(w io.Writer, tty bool) *importReporter {
	return &importReporter{w: w, tty: tty}
}

// Report prints one progress report.
func (r *importReporter) Report(p api.ImportProgress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := p.Phase != r.phase
	r.phase, r.seen = p.Phase, true
	switch p.Phase {
	case api.ImportPhaseVerify, api.ImportPhaseCheck, api.ImportPhasePublish:
		r.pastCopy = true
	}
	text := importProgressText(p)
	if r.tty {
		r.status(text)
		return
	}
	if changed || time.Since(r.printed) >= importLineInterval {
		r.printed = time.Now()
		fmt.Fprintln(r.w, text)
	}
}

// Seen reports whether any progress arrived. An older server sends none.
func (r *importReporter) Seen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

// PastCopy reports whether the copy phase has finished, from the progress seen.
func (r *importReporter) PastCopy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pastCopy
}

// Notice prints a message on its own line, below any status line.
func (r *importReporter) Notice(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeStatus()
	fmt.Fprintln(r.w, text)
}

// Write lets a logger share the stream: a log line starts below any status line.
func (r *importReporter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeStatus()
	return r.w.Write(p)
}

// Finish ends the status line so later output starts on a new line.
func (r *importReporter) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeStatus()
}

// status rewrites the status line: a carriage return, the text, and spaces over
// what the previous text left behind.
func (r *importReporter) status(text string) {
	width := ansi.StringWidth(text)
	pad := ""
	if r.width > width {
		pad = strings.Repeat(" ", r.width-width)
	}
	fmt.Fprint(r.w, "\r"+text+pad)
	r.width, r.open = width, true
}

func (r *importReporter) closeStatus() {
	if r.open {
		fmt.Fprintln(r.w)
		r.open, r.width = false, 0
	}
}

func importProgressText(p api.ImportProgress) string {
	switch p.Phase {
	case api.ImportPhaseLock:
		return config.Text("server.import_progress_lock")
	case api.ImportPhaseScan:
		if p.Total > 0 {
			return config.Text("server.import_progress_scan_count", p.Done, p.Total)
		}
		return config.Text("server.import_progress_scan")
	case api.ImportPhaseCopy:
		return config.Text("server.import_progress_copy", p.Done, p.Total, formatImportBytes(p.Bytes), formatImportBytes(p.TotalBytes))
	case api.ImportPhaseVerify:
		if p.Total > 0 {
			return config.Text("server.import_progress_verify_count", p.Done, p.Total)
		}
		return config.Text("server.import_progress_verify")
	case api.ImportPhaseCheck:
		return config.Text("server.import_progress_check")
	case api.ImportPhasePublish:
		return config.Text("server.import_progress_publish")
	}
	return p.Phase
}

func formatImportBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), "KMGT"
	for i := range suffix {
		value /= unit
		if value < unit || i == len(suffix)-1 {
			return fmt.Sprintf("%.1f %cB", value, suffix[i])
		}
	}
	return ""
}
