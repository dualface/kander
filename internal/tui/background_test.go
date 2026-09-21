package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestClassifyBackground(t *testing.T) {
	cases := []struct {
		payload string
		dark    bool
		ok      bool
	}{
		{"rgb:0000/0000/0000", true, true},
		{"rgb:ffff/ffff/ffff", false, true},
		{"rgb:ff/ff/ff", false, true},
		{"rgb:00/00/00", true, true},
		{"rgb:f/f/f", false, true},
		{"rgb:8000/8000/8000", false, true},
		{"rgb:0/0/0", true, true},
		{"rgb:ffff/0000/0000", false, true},
		{"rgb:0000/0000/ffff", false, true},
		{"RGB:FFFF/FFFF/FFFF", false, true},
		{"rgba:ffff/ffff/ffff/ffff", false, false},
		{"rgb:ffff/ffff", false, false},
		{"rgb:ffff/ffff/ffff/ffff", false, false},
		{"rgb:/0000/0000", false, false},
		{"rgb:fffff/0000/0000", false, false},
		{"rgb:xx/yy/zz", false, false},
		{"#aabbcc", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		dark, ok := classifyBackground([]byte(tc.payload))
		if dark != tc.dark || ok != tc.ok {
			t.Fatalf("classifyBackground(%q) = (%v,%v), want (%v,%v)", tc.payload, dark, ok, tc.dark, tc.ok)
		}
	}
}

func TestScanBackgroundResponse(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		consumed int
		dark     bool
		status   responseStatus
	}{
		{"bel-dark", "\x1b]11;rgb:0000/0000/0000\x07", 24, true, responseValid},
		{"bel-light", "\x1b]11;rgb:ffff/ffff/ffff\x07", 24, false, responseValid},
		{"st-dark", "\x1b]11;rgb:0000/0000/0000\x1b\\", 25, true, responseValid},
		{"st-light", "\x1b]11;rgb:fdfd/f6f6/e3e3\x1b\\", 25, false, responseValid},
		{"trailing-input", "\x1b]11;rgb:0000/0000/0000\x07abc", 24, true, responseValid},
		{"not-osc", "\x1b[31m", 0, false, responseNone},
		{"other-osc", "\x1b]8;;http://x\x07", 0, false, responseNone},
		{"prefix-esc", "\x1b", 0, false, responseNeedMore},
		{"prefix-part", "\x1b]1", 0, false, responseNeedMore},
		{"prefix-full", "\x1b]11;", 0, false, responseNeedMore},
		{"prefix-payload", "\x1b]11;rgb:0000/00", 0, false, responseNeedMore},
		{"prefix-st", "\x1b]11;rgb:0000/0000/0000\x1b", 0, false, responseNeedMore},
		{"bad-payload", "\x1b]11;nonsense\x07", 14, false, responseInvalid},
		{"bad-terminator", "\x1b]11;rgb:0000/0000/0000\x1bx", 24, false, responseInvalid},
		{"control-inside", "\x1b]11;rgb:\x01", 10, false, responseInvalid},
		{"esc-not-prefix", "\x1bx", 0, false, responseNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			consumed, dark, status := scanBackgroundResponse([]byte(tc.input))
			if consumed != tc.consumed || status != tc.status || (status == responseValid && dark != tc.dark) {
				t.Fatalf("scanBackgroundResponse(%q) = (%d,%v,%v), want (%d,%v,%v)", tc.input, consumed, dark, status, tc.consumed, tc.dark, tc.status)
			}
		})
	}
}

// testProbe returns a probe whose out writes to a pipe, plus the pipe ends.
// The probe is armed as if a query had just been issued.
func testProbe(t *testing.T, period, timeout time.Duration) (*backgroundProbe, *os.File, *os.File) {
	t.Helper()
	qr, qw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { qr.Close(); qw.Close() })
	bp := newBackgroundProbe(period, timeout, 40*time.Millisecond, 10*time.Second, true)
	bp.out = newProbeOutput(qw)
	return bp, qr, qw
}

// armOpenRound marks a query as issued and still outstanding.
func (bp *backgroundProbe) armOpenRound(d time.Duration) {
	bp.issued.Add(1)
	now := time.Now()
	bp.lastIssue.Store(now.UnixNano())
	bp.deadline.Store(now.Add(d).UnixNano())
}

func drainQueries(f *os.File) string {
	out := ""
	buf := make([]byte, 256)
	_ = f.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
	for {
		n, err := f.Read(buf)
		if n > 0 {
			out += string(buf[:n])
		}
		if err != nil {
			return out
		}
	}
}

func TestProbeTickIssuesQueryAtPeriod(t *testing.T) {
	bp, qr, _ := testProbe(t, 50*time.Millisecond, 30*time.Millisecond)
	bp.enabled.Store(true)
	now := time.Now()
	bp.tick(now)
	if got := drainQueries(qr); got != backgroundOSCQuery {
		t.Fatalf("first tick must issue one query, wrote %q", got)
	}
	// While the round is outstanding, later ticks issue nothing.
	bp.tick(now.Add(10 * time.Millisecond))
	if got := drainQueries(qr); got != "" {
		t.Fatalf("outstanding round must not issue another query, wrote %q", got)
	}
	// After the timeout the round expires and the next due tick issues again.
	bp.tick(now.Add(40 * time.Millisecond))
	bp.tick(now.Add(60 * time.Millisecond))
	if got := drainQueries(qr); got != backgroundOSCQuery {
		t.Fatalf("expired round must allow a new query, wrote %q", got)
	}
}

func TestProbeTickAppliesCapturedResponse(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	bp, _, _ := testProbe(t, time.Second, time.Second)
	bp.enabled.Store(true)
	bp.armOpenRound(time.Second)
	bp.capture(true, true)
	if _, ok := runtimeBackground(); ok {
		t.Fatal("capture alone must not change the applied classification")
	}
	bp.tick(time.Now())
	dark, ok := runtimeBackground()
	if !ok || !dark {
		t.Fatal("captured dark response must apply on the next tick")
	}
}

func TestProbeTickDropsStaleAndInvalidResults(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	bp, _, _ := testProbe(t, time.Second, time.Second)
	bp.enabled.Store(true)
	bp.armOpenRound(time.Second)
	// An invalid response closes the round without changing the theme.
	bp.capture(false, false)
	bp.tick(time.Now())
	if _, ok := runtimeBackground(); ok {
		t.Fatal("invalid response must not apply a classification")
	}
	// A late capture without an open round is dropped entirely.
	bp.capture(true, true)
	bp.tick(time.Now())
	if _, ok := runtimeBackground(); ok {
		t.Fatal("stale response must not apply a classification")
	}
	// An older generation never moves a newer applied result backwards.
	bp.armOpenRound(time.Second)
	bp.capture(true, true)
	bp.tick(time.Now())
	bp.pendingGen.Store(bp.appliedGen - 1)
	bp.pendingDark.Store(false)
	bp.pendingOk.Store(true)
	bp.tick(time.Now())
	dark, ok := runtimeBackground()
	if !ok || !dark {
		t.Fatal("an older generation must not revert the newer classification")
	}
}

func TestProbeDisabledIssuesNothing(t *testing.T) {
	bp, qr, _ := testProbe(t, time.Millisecond, time.Second)
	bp.tick(time.Now())
	if got := drainQueries(qr); got != "" {
		t.Fatalf("disabled probe must not query, wrote %q", got)
	}
}

// pipeInput returns a probeInput reading one end of a pipe and the write end.
func pipeInput(t *testing.T, bp *backgroundProbe) (*probeInput, *os.File) {
	t.Helper()
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close(); wr.Close() })
	return newProbeInput(rd, bp), wr
}

func readAll(r *probeInput, want int, timeout time.Duration) (string, error) {
	out := make([]byte, 0, want)
	buf := make([]byte, 64)
	deadline := time.Now().Add(timeout)
	for len(out) < want {
		n, err := r.Read(buf)
		if err != nil {
			return string(out), err
		}
		out = append(out, buf[:n]...)
		if time.Now().After(deadline) {
			return string(out), errTimeout
		}
	}
	return string(out), nil
}

var errTimeout = errorString("probe input read timed out")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestProbeInputPassesInputUnchanged(t *testing.T) {
	bp := newBackgroundProbe(time.Second, time.Second, 40*time.Millisecond, 10*time.Second, true)
	in, wr := pipeInput(t, bp)
	want := "plain input \x1b[A more"
	if _, err := wr.WriteString(want); err != nil {
		t.Fatal(err)
	}
	got, err := readAll(in, len(want), 2*time.Second)
	if err != nil || got != want {
		t.Fatalf("passthrough = %q err=%v, want %q", got, err, want)
	}
}

func TestProbeInputStripsResponseAndKeepsKeys(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	bp, _, _ := testProbe(t, time.Second, 5*time.Second)
	bp.armOpenRound(5 * time.Second)
	in, wr := pipeInput(t, bp)
	_, err := wr.WriteString("a\x1b]11;rgb:ffff/ffff/ffff\x1b\\b")
	if err != nil {
		t.Fatal(err)
	}
	got, err := readAll(in, 2, 2*time.Second)
	if err != nil || got != "ab" {
		t.Fatalf("keys around a response = %q err=%v, want %q", got, err, "ab")
	}
	bp.tick(time.Now())
	dark, ok := runtimeBackground()
	if !ok || dark {
		t.Fatalf("stripped light response must apply as light, got dark=%v ok=%v", dark, ok)
	}
}

func TestProbeInputAssemblesSplitResponse(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	bp, _, _ := testProbe(t, time.Second, 5*time.Second)
	bp.armOpenRound(5 * time.Second)
	in, wr := pipeInput(t, bp)
	first := "ab\x1b]11;rg"
	if _, err := wr.WriteString(first); err != nil {
		t.Fatal(err)
	}
	got, err := readAll(in, 2, 2*time.Second)
	if err != nil || got != "ab" {
		t.Fatalf("first chunk must emit only the keys, got %q err=%v", got, err)
	}
	if _, err := wr.WriteString("b:0000/0000/0000\x07c"); err != nil {
		t.Fatal(err)
	}
	got, err = readAll(in, 1, 2*time.Second)
	if err != nil || got != "c" {
		t.Fatalf("split response must be stripped, got %q err=%v", got, err)
	}
	bp.tick(time.Now())
	dark, ok := runtimeBackground()
	if !ok || !dark {
		t.Fatalf("split dark response must apply as dark, got dark=%v ok=%v", dark, ok)
	}
}

func TestProbeInputFlushesHeldFragment(t *testing.T) {
	bp, _, _ := testProbe(t, time.Second, 5*time.Second)
	bp.holdFor = 30 * time.Millisecond
	bp.armOpenRound(5 * time.Second)
	in, wr := pipeInput(t, bp)
	frag := "\x1b]11;rg"
	if _, err := wr.WriteString(frag); err != nil {
		t.Fatal(err)
	}
	got, err := readAll(in, len(frag), 2*time.Second)
	if err != nil || got != frag {
		t.Fatalf("incomplete fragment must be flushed after holdFor, got %q err=%v", got, err)
	}
}

func TestProbeInputKeepsPasteBytes(t *testing.T) {
	bp, _, _ := testProbe(t, time.Second, 5*time.Second)
	bp.armOpenRound(5 * time.Second)
	in, wr := pipeInput(t, bp)
	paste := "\x1b[200~text \x1b]11;rgb:0000/0000/0000\x07 content\x1b[201~"
	if _, err := wr.WriteString(paste); err != nil {
		t.Fatal(err)
	}
	got, err := readAll(in, len(paste), 2*time.Second)
	if err != nil || got != paste {
		t.Fatalf("pasted bytes must pass through untouched, got %q err=%v", got, err)
	}
	if bp.pendingGen.Load() != 0 {
		t.Fatal("a response inside bracketed paste must not reach the probe")
	}
}

func TestProbeInputKeepsSplitPasteBytes(t *testing.T) {
	bp, _, _ := testProbe(t, time.Second, 5*time.Second)
	bp.armOpenRound(5 * time.Second)
	in, wr := pipeInput(t, bp)
	// The paste start marker and a response-looking payload are both split;
	// pasted bytes must pass through untouched and never reach the probe.
	chunks := []string{"\x1b[20", "0~text ", "\x1b]11;rgb:0000/0000/0000\x07", " more\x1b[201", "~"}
	for _, chunk := range chunks {
		if _, err := wr.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond)
	}
	want := strings.Join(chunks, "")
	got, err := readAll(in, len(want), 2*time.Second)
	if err != nil || got != want {
		t.Fatalf("split paste must pass through untouched, got %q err=%v want %q", got, err, want)
	}
	if bp.pendingGen.Load() != 0 {
		t.Fatal("pasted bytes must never reach the probe")
	}
}

func TestProbeInputStripsInvalidResponse(t *testing.T) {
	bp, _, _ := testProbe(t, time.Second, 5*time.Second)
	bp.armOpenRound(5 * time.Second)
	in, wr := pipeInput(t, bp)
	if _, err := wr.WriteString("x\x1b]11;rgba:ffff\x07y"); err != nil {
		t.Fatal(err)
	}
	got, err := readAll(in, 2, 2*time.Second)
	if err != nil || got != "xy" {
		t.Fatalf("invalid response must be stripped, got %q err=%v", got, err)
	}
	if bp.pendingGen.Load() == 0 || bp.pendingOk.Load() {
		t.Fatal("invalid response must close the round as invalid")
	}
}

func TestProbeTickSwitchesResolvedTheme(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	app := newApp(false, 30, tuiPageContext(), nil, nil, "auto", 3, nil, nil)
	bp, qr, _ := testProbe(t, time.Millisecond, 5*time.Second)
	app.probe = bp
	if app.resolvedTheme == "" {
		app.resolvedTheme = resolveTheme(app.Theme)
	}
	app.probeTick()
	if got := drainQueries(qr); got != backgroundOSCQuery {
		t.Fatalf("auto theme must probe, wrote %q", got)
	}
	bp.capture(true, true)
	app.probeTick()
	if app.resolvedTheme != "dark" {
		t.Fatalf("resolved theme must switch to dark, got %q", app.resolvedTheme)
	}
}

func TestProbeTickSkipsNamedTheme(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	app := newApp(false, 30, tuiPageContext(), nil, nil, "dark", 3, nil, nil)
	bp, qr, _ := testProbe(t, time.Millisecond, 5*time.Second)
	app.probe = bp
	app.probeTick()
	if got := drainQueries(qr); got != "" {
		t.Fatalf("named theme must not probe, wrote %q", got)
	}
	if app.resolvedTheme != "dark" {
		t.Fatalf("named theme must stay fixed, got %q", app.resolvedTheme)
	}
}

func TestResolveThemePrefersRuntimeBackground(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	original := detectDarkBackground
	t.Cleanup(func() { detectDarkBackground = original })
	detectDarkBackground = func() bool { return false }
	if got := resolveTheme("auto"); got != "light" {
		t.Fatalf("startup fallback must apply before runtime results, got %q", got)
	}
	autoBackground.Store(2<<1 | 1)
	if got := resolveTheme("auto"); got != "dark" {
		t.Fatalf("runtime dark must win over the startup probe, got %q", got)
	}
	autoBackground.Store(3 << 1)
	if got := resolveTheme("auto"); got != "light" {
		t.Fatalf("runtime light must win over the startup probe, got %q", got)
	}
	if got := resolveTheme("dark"); got != "dark" {
		t.Fatalf("named themes never consult probing, got %q", got)
	}
	if got := resolveTheme("tide"); got != "tide" {
		t.Fatalf("named themes never consult probing, got %q", got)
	}
}

func TestApplyThemeSwitchUpdatesHuhForms(t *testing.T) {
	t.Cleanup(resetAutoBackground)
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	app := newApp(false, 30, tuiPageContext(), nil, nil, "auto", 3, nil, nil)
	dialog := &taskActions{}
	dialog.formTheme = huhTheme(themePalette("light"))
	app.TaskActions = dialog
	autoBackground.Store(4<<1 | 1)
	app.applyThemeSwitch()
	want := termenv.TrueColor.Color(string(themePalette("dark").Bg)).Sequence(true)
	if got := dialog.formTheme.Focused.Base.Render("x"); !strings.Contains(got, want) {
		t.Fatalf("task action form theme must follow the resolved palette, want %q in %q", want, got)
	}
}
