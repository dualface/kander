package tui

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Runtime terminal background probing for the "auto" theme.
//
// Lip Gloss answers HasDarkBackground once per process (termenv uses a
// sync.Once), so an "auto" theme picked at startup never follows a terminal
// that changes its background while Kander keeps running. Bubble Tea v1 has no
// background-color message, and a second raw termenv query would compete with
// Bubble Tea's own stdin reads, so the probe lives on the TUI I/O boundary
// instead:
//
//   - runTUI wraps os.Stdout in probeOutput; the UI goroutine injects an
//     OSC 11 query through the same mutex the renderer writes through, so a
//     query can never split a frame.
//   - runTUI wraps os.Stdin in probeInput; every byte reaching Bubble Tea's
//     key parser passes its scanner, which strips complete OSC 11 responses
//     before they could be parsed as phantom key presses.
//   - probeTick drives one round at a time from the UI goroutine: a response
//     is recorded by the reader and applied by the next tick, so the stored
//     classification only changes between Update calls and one rendered frame
//     can never mix two palettes.
//
// Rounds are serialized: at most one query is outstanding. A response is bound
// to whatever round is open when it arrives; a terminal that answers after the
// timeout has its bytes stripped by the strip window but discarded for state,
// so a late or reordered result cannot move the theme backwards.

const (
	backgroundProbePeriod     = 1 * time.Second
	backgroundProbeTimeout    = 250 * time.Millisecond
	backgroundProbeHold       = 50 * time.Millisecond
	backgroundProbeStripGrace = 10 * time.Second
	backgroundOSCQuery        = "\x1b]11;?\x1b\\"
	backgroundOSCPrefix       = "\x1b]11;"
	backgroundMaxPayload      = 64
)

// autoBackground is the last runtime classification applied by the probe.
// A negative value means no runtime result yet: resolveTheme then falls back
// to the one-time startup probe. Non-negative values pack (generation<<1)|dark.
var autoBackground atomic.Int64

func init() {
	autoBackground.Store(-1)
}

// resetAutoBackground clears the runtime classification; tests use it so the
// startup fallback stays deterministic between cases.
func resetAutoBackground() {
	autoBackground.Store(-1)
}

// runtimeBackground reports the probed classification, or ok=false when the
// runtime probe has not produced one and the caller should use the startup
// fallback.
func runtimeBackground() (dark, ok bool) {
	v := autoBackground.Load()
	if v < 0 {
		return false, false
	}
	return v&1 == 1, true
}

// backgroundProbe owns the round state of the runtime background query. The
// UI goroutine drives tick; the input reader reports responses through
// capture; all mutation of the shared classification happens inside tick so
// the stored theme only changes between frames.
type backgroundProbe struct {
	period     time.Duration
	timeout    time.Duration
	holdFor    time.Duration
	stripGrace time.Duration
	supported  bool
	out        *probeOutput

	enabled     atomic.Bool
	issued      atomic.Int64
	deadline    atomic.Int64 // unixnano of the open round's expiry, 0 when none
	lastIssue   atomic.Int64 // unixnano of the last issued query
	pendingGen  atomic.Int64
	pendingDark atomic.Bool
	pendingOk   atomic.Bool

	appliedGen int64 // UI goroutine only: generation of the last consumed result
}

func newBackgroundProbe(period, timeout, holdFor, stripGrace time.Duration, supported bool) *backgroundProbe {
	return &backgroundProbe{
		period:     period,
		timeout:    timeout,
		holdFor:    holdFor,
		stripGrace: stripGrace,
		supported:  supported,
	}
}

// probeSupported reports whether this process can probe the terminal
// background: both streams must be terminals and the platform must deliver
// terminal replies as an input byte stream. Windows consoles report input as
// event records rather than an ANSI stream, so the probe stays off there and
// "auto" keeps the startup fallback.
func probeSupported() bool {
	return waitReadableWorks && isTTY(os.Stdin) && isTTY(os.Stdout)
}

// expecting reports whether an OSC 11 response may still be in flight: either
// a round is open or the last query was issued within the strip grace. Past
// the grace the scanner passes input through untouched, so unrelated bytes
// can never be mistaken for a response.
func (bp *backgroundProbe) expecting(now time.Time) bool {
	last := bp.lastIssue.Load()
	if last == 0 {
		return false
	}
	if bp.deadline.Load() != 0 {
		return true
	}
	return now.UnixNano()-last < int64(bp.stripGrace)
}

// capture records one response from the input reader. Only an open round may
// be answered: a response arriving after its deadline is stale and dropped.
// The reader goroutine writes the payload before the generation, so the UI
// goroutine that sees the generation also sees the payload.
func (bp *backgroundProbe) capture(dark, ok bool) {
	if bp.deadline.Load() == 0 {
		return
	}
	bp.pendingDark.Store(dark)
	bp.pendingOk.Store(ok)
	bp.pendingGen.Store(bp.issued.Load())
	bp.deadline.Store(0)
}

// tick runs the probe once per UI tick: it consumes a pending response,
// expires an unanswered round, and issues the next query when due. It must be
// called on the same goroutine as Update so the applied classification never
// changes mid-render.
func (bp *backgroundProbe) tick(now time.Time) {
	if gen := bp.pendingGen.Swap(0); gen > bp.appliedGen {
		bp.appliedGen = gen
		if bp.pendingOk.Load() {
			dark := int64(0)
			if bp.pendingDark.Load() {
				dark = 1
			}
			cur := autoBackground.Load()
			// Only a newer round may move the stored classification.
			if cur < 0 || gen > cur>>1 {
				autoBackground.Store(gen<<1 | dark)
			}
		}
	}
	if !bp.enabled.Load() || bp.out == nil {
		return
	}
	if deadline := bp.deadline.Load(); deadline != 0 {
		if now.UnixNano() < deadline {
			return
		}
		bp.deadline.Store(0)
	}
	if now.UnixNano()-bp.lastIssue.Load() < int64(bp.period) {
		return
	}
	bp.issued.Add(1)
	bp.lastIssue.Store(now.UnixNano())
	bp.deadline.Store(now.Add(bp.timeout).UnixNano())
	bp.out.sendQuery()
}

// probeTick drives the background probe from the UI tick and propagates a
// resolved-theme change to theme-carrying dialogs.
func (a *App) probeTick() {
	bp := a.probe
	if bp == nil || !bp.supported {
		return
	}
	// Named themes are fixed by the user and never probed; "auto" (and any
	// name resolveTheme treats as auto) enables the probe.
	_, named := themeDefByName(a.Theme)
	bp.enabled.Store(!named)
	bp.tick(a.Now())
	resolved := resolveTheme(a.Theme)
	if resolved != a.resolvedTheme {
		a.resolvedTheme = resolved
		a.applyThemeSwitch()
	}
}

// applyThemeSwitch refreshes theme state held outside per-frame rendering:
// the Huh forms keep a *huh.Theme whose styles are updated in place, and their
// fields pick the new values up on the next form update.
func (a *App) applyThemeSwitch() {
	p := themePalette(a.Theme)
	if a.Options != nil {
		a.Options.syncFormTheme(p)
	}
	if a.TaskActions != nil && a.TaskActions.formTheme != nil {
		applyHuhPalette(a.TaskActions.formTheme, p)
	}
}

// probeOutput wraps the terminal writer: the renderer and the query injector
// share one mutex, so a probe query can never split a rendered frame.
type probeOutput struct {
	file *os.File
	mu   sync.Mutex
}

func newProbeOutput(file *os.File) *probeOutput {
	return &probeOutput{file: file}
}

func (w *probeOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Write(p)
}

// Read satisfies term.File; the output side never reads.
func (w *probeOutput) Read(p []byte) (int, error) { return w.file.Read(p) }

// Close satisfies term.File; closing stdout is never allowed.
func (w *probeOutput) Close() error { return nil }

// Fd keeps the wrapper a term.File so Bubble Tea still detects the terminal.
func (w *probeOutput) Fd() uintptr { return w.file.Fd() }

func (w *probeOutput) sendQuery() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.file.WriteString(backgroundOSCQuery)
}

// probeInput wraps the terminal reader: it scans the byte stream for OSC 11
// responses, strips them before Bubble Tea's key parser can see them, and
// reports the classification to the probe. All other bytes pass through
// unchanged, including escape sequences that merely share the ESC byte.
type probeInput struct {
	file  *os.File
	probe *backgroundProbe
	buf   [256]byte
	// hold is the trailing fragment that may be the start of a response; it is
	// flushed to plain after holdFor when the rest never arrives.
	hold   []byte
	holdAt time.Time
	// inPaste tracks bracketed paste so pasted text is never scanned.
	inPaste bool
	plain   []byte
	err     error
}

func newProbeInput(file *os.File, probe *backgroundProbe) *probeInput {
	return &probeInput{file: file, probe: probe}
}

// Fd keeps the wrapper a term.File: Bubble Tea puts the same descriptor into
// raw mode and the cancel reader polls it, while all reads still pass through
// this wrapper's scanner.
func (r *probeInput) Fd() uintptr { return r.file.Fd() }

// Write satisfies term.File; stdin is never written.
func (r *probeInput) Write(p []byte) (int, error) { return r.file.Write(p) }

// Name satisfies the cancel reader's File interface.
func (r *probeInput) Name() string { return r.file.Name() }

// Close satisfies term.File; closing stdin is never allowed.
func (r *probeInput) Close() error { return nil }

// Read returns the next chunk of user input with terminal responses removed.
// A trailing fragment that may be a response start is held only while a
// response is expected, and only for holdFor, so keys (including a lone ESC)
// are never delayed beyond that bound.
func (r *probeInput) Read(p []byte) (int, error) {
	for {
		if len(r.plain) > 0 {
			n := copy(p, r.plain)
			r.plain = r.plain[n:]
			return n, nil
		}
		if r.err != nil {
			return 0, r.err
		}
		if len(r.hold) > 0 {
			wait := r.probe.holdFor - time.Since(r.holdAt)
			if !r.probe.expecting(time.Now()) || wait <= 0 || !waitReadable(r.file.Fd(), wait) {
				r.plain = append(r.plain, r.hold...)
				r.hold = r.hold[:0]
				continue
			}
		}
		n, err := r.file.Read(r.buf[:])
		if err != nil {
			r.err = err
			r.plain = append(r.plain, r.hold...)
			r.hold = r.hold[:0]
			continue
		}
		if n == 0 {
			continue
		}
		chunk := r.buf[:n]
		if !r.probe.expecting(time.Now()) && len(r.hold) == 0 {
			r.plain = append(r.plain, chunk...)
			continue
		}
		r.scan(chunk)
	}
}

// scan walks one input chunk, strips complete background responses, reports
// them, and keeps a possible response prefix in hold for the next chunk.
func (r *probeInput) scan(chunk []byte) {
	buf := make([]byte, 0, len(r.hold)+len(chunk))
	buf = append(buf, r.hold...)
	buf = append(buf, chunk...)
	r.hold = r.hold[:0]
	for i := 0; i < len(buf); {
		if r.inPaste {
			if bytes.HasPrefix(buf[i:], []byte("\x1b[201~")) {
				r.inPaste = false
				r.plain = append(r.plain, buf[i:i+len("\x1b[201~")]...)
				i += len("\x1b[201~")
				continue
			}
			if buf[i] == 0x1b && bytes.HasPrefix([]byte("\x1b[201~"), buf[i:]) {
				// The paste end marker is split across chunks; hold the
				// fragment so the marker still closes the paste.
				r.hold = append(r.hold, buf[i:]...)
				r.holdAt = time.Now()
				return
			}
			r.plain = append(r.plain, buf[i])
			i++
			continue
		}
		if buf[i] != 0x1b {
			next := bytes.IndexByte(buf[i:], 0x1b)
			if next < 0 {
				next = len(buf) - i
			}
			r.plain = append(r.plain, buf[i:i+next]...)
			i += next
			continue
		}
		if bytes.HasPrefix(buf[i:], []byte("\x1b[200~")) {
			r.inPaste = true
			r.plain = append(r.plain, buf[i:i+len("\x1b[200~")]...)
			i += len("\x1b[200~")
			continue
		}
		if bytes.HasPrefix([]byte("\x1b[200~"), buf[i:]) {
			// The paste start marker is split across chunks; hold the
			// fragment so pasted content is never scanned as a response.
			r.hold = append(r.hold, buf[i:]...)
			r.holdAt = time.Now()
			return
		}
		consumed, dark, status := scanBackgroundResponse(buf[i:])
		switch status {
		case responseNeedMore:
			if !r.probe.expecting(time.Now()) {
				// No response is expected, so a partial prefix is user input;
				// only the ESC byte goes out and the rest is rescanned.
				r.plain = append(r.plain, buf[i])
				i++
				continue
			}
			r.hold = append(r.hold, buf[i:]...)
			r.holdAt = time.Now()
			return
		case responseValid:
			r.probe.capture(dark, true)
			i += consumed
		case responseInvalid:
			r.probe.capture(false, false)
			i += consumed
		default:
			r.plain = append(r.plain, buf[i])
			i++
		}
	}
}

type responseStatus int

const (
	responseNone responseStatus = iota
	responseNeedMore
	responseValid
	responseInvalid
)

// scanBackgroundResponse matches one OSC 11 reply at the start of b.
// Responses look like "\x1b]11;rgb:ffff/0000/ffff" terminated by BEL or ST.
// needMore means the bytes so far are a strict prefix; invalid means the
// sequence consumed is a terminal response that does not carry a usable
// rgb: payload, so the caller drops it rather than leaking it to key parsing.
func scanBackgroundResponse(b []byte) (consumed int, dark bool, status responseStatus) {
	if len(b) < len(backgroundOSCPrefix) {
		if strings.HasPrefix(backgroundOSCPrefix, string(b)) {
			return 0, false, responseNeedMore
		}
		return 0, false, responseNone
	}
	if !strings.HasPrefix(string(b[:len(backgroundOSCPrefix)]), backgroundOSCPrefix) {
		return 0, false, responseNone
	}
	for i := len(backgroundOSCPrefix); i < len(b); i++ {
		c := b[i]
		switch {
		case c == 0x07:
			dark, ok := classifyBackground(b[len(backgroundOSCPrefix):i])
			if !ok {
				return i + 1, false, responseInvalid
			}
			return i + 1, dark, responseValid
		case c == 0x1b:
			if i+1 >= len(b) {
				return 0, false, responseNeedMore
			}
			if b[i+1] != '\\' {
				return i + 1, false, responseInvalid
			}
			dark, ok := classifyBackground(b[len(backgroundOSCPrefix):i])
			if !ok {
				return i + 2, false, responseInvalid
			}
			return i + 2, dark, responseValid
		case c >= 0x20 && c <= 0x7e:
			if i-len(backgroundOSCPrefix) >= backgroundMaxPayload {
				return i + 1, false, responseInvalid
			}
		default:
			return i + 1, false, responseInvalid
		}
	}
	return 0, false, responseNeedMore
}

// classifyBackground parses an OSC 11 payload of the form rgb:R/G/B, where
// each component carries one to four hex digits, and reports whether the
// color is dark by HSL lightness, matching termenv's threshold.
func classifyBackground(payload []byte) (dark, ok bool) {
	s := string(payload)
	if !strings.HasPrefix(strings.ToLower(s), "rgb:") {
		return false, false
	}
	parts := strings.Split(s[4:], "/")
	if len(parts) != 3 {
		return false, false
	}
	var components [3]int
	for i, part := range parts {
		if len(part) < 1 || len(part) > 4 {
			return false, false
		}
		value := 0
		for _, c := range []byte(part) {
			value *= 16
			switch {
			case c >= '0' && c <= '9':
				value += int(c - '0')
			case c >= 'a' && c <= 'f':
				value += int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				value += int(c-'A') + 10
			default:
				return false, false
			}
		}
		// termenv reduces the reply to its top byte, so the same boundary
		// values classify identically here: longer components keep their
		// first two hex digits, a single digit is a replicated nibble.
		switch len(part) {
		case 1:
			components[i] = value * 17
		case 2:
			components[i] = value
		default:
			components[i] = value >> (4 * (len(part) - 2))
		}
	}
	hi, lo := components[0], components[0]
	for _, v := range components[1:] {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	// HSL lightness below one half counts as dark, like lipgloss.
	return hi+lo < 255, true
}
