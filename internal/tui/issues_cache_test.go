package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/issue"
)

// issuesClock controls the app clock so the debounce is deterministic: the
// overlay tick runs against this clock instead of sleeping on the UI grid.
type issuesClock struct {
	app *App
	now time.Time
}

func freezeIssuesClock(t *testing.T, app *App) *issuesClock {
	t.Helper()
	now := time.Now()
	clock := &issuesClock{app: app, now: now}
	app.Now = func() time.Time { return clock.now }
	app.LastRefresh = now
	return clock
}

// tick advances one full UI interval and runs the overlay tick handling. The
// queued work is handed off by runPendingWork, exactly like program.Update does.
func (c *issuesClock) tick() {
	c.now = c.now.Add(uiTickInterval)
	c.app.issuesTick()
}

// applyWorkCmd runs one queued command that carries a workMsg, so a test can
// land a specific in-flight result out of order.
func applyWorkCmd(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no pending work was queued")
	}
	message := cmd()
	work, ok := message.(workMsg)
	if !ok {
		t.Fatalf("unexpected message %T", message)
	}
	app.applyWork(work.payload)
}

// cacheTestApp wires the overlay to one cached snapshot and records every cache
// write, so a test can tell a hit, an unchanged refresh and an update apart.
func cacheTestApp(t *testing.T, fake *fakeIssues, cached issue.IssueSnapshot) (*App, *issuesClock, *[]issue.IssueSnapshot) {
	t.Helper()
	app := issuesTestApp(t, fake, 120, 30)
	clock := freezeIssuesClock(t, app)
	saved := &[]issue.IssueSnapshot{}
	app.LoadIssueCache = func(_ issue.Repository, number int) (issue.IssueSnapshot, bool) {
		if number != cached.Number {
			return issue.IssueSnapshot{}, false
		}
		return cached, true
	}
	app.SaveIssueCache = func(snapshot issue.IssueSnapshot) error {
		*saved = append(*saved, snapshot)
		return nil
	}
	return app, clock, saved
}

// Moving the selection repeatedly inside one tick window must produce exactly
// one network request, and the first selection waits for a stable tick too.
func TestIssuesSelectionDebouncesContentRequests(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(_ int, number int, _ bool) (issue.IssueSnapshot, error) {
		snapshot := defaultSnapshot(t)
		snapshot.Number = number
		return snapshot, nil
	}
	app := issuesTestApp(t, fake, 120, 30)
	clock := freezeIssuesClock(t, app)
	app.HandleKey("g")
	runPendingWork(t, app)

	if fake.getCalls != 0 || app.pendingWork != nil {
		t.Fatalf("content request started before a tick: calls=%d pending=%v", fake.getCalls, app.pendingWork != nil)
	}
	clock.tick()
	runPendingWork(t, app)
	if fake.getCalls != 1 || fake.numbers[0] != 42 {
		t.Fatalf("first request: calls=%d numbers=%v", fake.getCalls, fake.numbers)
	}

	// A burst of movement resets the debounce; only the final target is sent.
	app.HandleKey("down")
	clock.now = clock.now.Add(uiTickInterval / 4)
	app.issuesTick()
	app.HandleKey("up")
	clock.now = clock.now.Add(uiTickInterval / 4)
	app.issuesTick()
	app.HandleKey("down")
	if app.pendingWork != nil {
		t.Fatal("a request started while the selection was still moving")
	}
	clock.tick()
	runPendingWork(t, app)
	if fake.getCalls != 2 || fake.numbers[1] != 41 {
		t.Fatalf("burst requests: calls=%d numbers=%v", fake.getCalls, fake.numbers)
	}
}

// A selection that does not survive one full tick is never fetched.
func TestIssuesSelectionWaitsForAStableTick(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	app := issuesTestApp(t, fake, 120, 30)
	clock := freezeIssuesClock(t, app)
	app.HandleKey("g")
	runPendingWork(t, app)
	app.HandleKey("down")

	clock.now = clock.now.Add(uiTickInterval / 2)
	app.issuesTick()
	if app.pendingWork != nil {
		t.Fatal("the armed selection fired before a full stable tick")
	}
	clock.tick()
	runPendingWork(t, app)
	if fake.getCalls != 1 || fake.numbers[0] != 41 {
		t.Fatalf("calls=%d numbers=%v", fake.getCalls, fake.numbers)
	}
}

// A cache hit paints the content before the background refresh, and an
// unchanged refresh keeps the scroll position and the cache file.
func TestIssuesCacheHitPaintsBeforeTheRefresh(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	cached := defaultSnapshot(t)
	fake.getResult = func(int, int, bool) (issue.IssueSnapshot, error) { return cached, nil }
	app, clock, saved := cacheTestApp(t, fake, cached)

	app.HandleKey("g")
	runPendingWork(t, app)
	st := app.Issues
	if st.detail == nil || st.detail.Number != 42 {
		t.Fatalf("cached detail missing: %+v", st.detail)
	}
	if !strings.Contains(ansi.Strip(app.View()), "Steps to reproduce") {
		t.Fatal("the cached body is not on screen")
	}
	if fake.getCalls != 0 {
		t.Fatal("a cache hit still blocked on the network")
	}

	st.detailScroll = 3
	clock.tick()
	runPendingWork(t, app)
	if fake.getCalls != 1 {
		t.Fatalf("refresh calls=%d", fake.getCalls)
	}
	if st.detailScroll != 3 {
		t.Fatalf("an unchanged refresh reset the scroll to %d", st.detailScroll)
	}
	if len(*saved) != 0 {
		t.Fatalf("an unchanged refresh rewrote the cache: %+v", *saved)
	}
	if st.notice != "" {
		t.Fatalf("an unchanged refresh produced a notice: %q", st.notice)
	}
}

// A changed refresh writes the cache, repaints and reports the update.
func TestIssuesChangedRefreshUpdatesCacheAndNotice(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	cached := defaultSnapshot(t)
	cached.Body = "OLD-BODY"
	refreshed := defaultSnapshot(t)
	refreshed.Body = "NEW-BODY"
	fake.getResult = func(int, int, bool) (issue.IssueSnapshot, error) { return refreshed, nil }
	app, clock, saved := cacheTestApp(t, fake, cached)

	app.HandleKey("g")
	runPendingWork(t, app)
	if app.Issues.detail == nil || !strings.Contains(app.Issues.detail.Body, "OLD-BODY") {
		t.Fatalf("cached detail: %+v", app.Issues.detail)
	}
	clock.tick()
	runPendingWork(t, app)
	st := app.Issues
	if st.detail == nil || !strings.Contains(st.detail.Body, "NEW-BODY") {
		t.Fatalf("the refresh did not replace the body: %+v", st.detail)
	}
	if len(*saved) != 1 || !strings.Contains((*saved)[0].Body, "NEW-BODY") {
		t.Fatalf("a changed refresh did not rewrite the cache: %+v", *saved)
	}
	if st.notice != app.Context.IssuesUpdated {
		t.Fatalf("notice=%q want %q", st.notice, app.Context.IssuesUpdated)
	}
	if !strings.Contains(ansi.Strip(app.View()), app.Context.IssuesUpdated) {
		t.Fatal("the update notice is not visible")
	}
	if st.detailScroll != 0 {
		t.Fatalf("changed content left the scroll at %d", st.detailScroll)
	}
}

// A list reload invalidates an in-flight content request: its result is
// dropped, the loading state clears, and the tick fetches the current
// selection again.
func TestIssuesListReloadInvalidatesTheContentRequest(t *testing.T) {
	fake := newFakeIssues()
	fake.listResult = defaultPage(issuesListLimit)
	fake.getResult = func(int, int, bool) (issue.IssueSnapshot, error) { return defaultSnapshot(t), nil }
	app := issuesTestApp(t, fake, 120, 30)
	clock := freezeIssuesClock(t, app)
	app.HandleKey("g")
	runPendingWork(t, app)
	clock.tick()
	inFlight := app.takePending()
	if inFlight == nil {
		t.Fatal("no content request was queued")
	}
	app.HandleKey("r")
	if app.pendingWork == nil {
		t.Fatal("the refresh did not queue the list request")
	}
	runPendingWork(t, app)
	if !app.Issues.detailLoading {
		t.Fatal("the invalidated content request must keep its flight state")
	}
	applyWorkCmd(t, app, inFlight)
	if app.Issues.detail != nil {
		t.Fatalf("an invalidated content request landed: %+v", app.Issues.detail)
	}
	if app.Issues.detailLoading {
		t.Fatal("the loading state leaked")
	}
	clock.tick()
	runPendingWork(t, app)
	if app.Issues.detail == nil || app.Issues.detail.Number != 42 {
		t.Fatalf("the refreshed detail is missing: %+v", app.Issues.detail)
	}
}
