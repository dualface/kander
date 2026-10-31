//go:build unix

package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

const leaderPID = 4242

func fakeGroup() *launchedProcess {
	return &launchedProcess{cmd: &exec.Cmd{Process: &os.Process{Pid: leaderPID}}}
}

func member(start string) procInfo {
	return procInfo{ppid: 1, pgid: leaderPID, startTime: start, executable: "sleep"}
}

// injectTables serves the tables in order and repeats the last one.
func injectTables(t *testing.T, tables ...map[int]procInfo) *int {
	t.Helper()
	calls := 0
	injectRead(t, func(context.Context) (map[int]procInfo, error) {
		table := tables[min(calls, len(tables)-1)]
		calls++
		return table, nil
	})
	return &calls
}

func injectRead(t *testing.T, read func(context.Context) (map[int]procInfo, error)) {
	t.Helper()
	old := readProcessTable
	readProcessTable = read
	t.Cleanup(func() { readProcessTable = old })
}

func outcomes(e *processEvidence) map[string]string {
	result := map[string]string{}
	for _, m := range e.Members {
		result[fmt.Sprintf("%d/%s", m.PID, m.StartTime)] = m.Outcome
	}
	return result
}

func archivedEvidence(t *testing.T, root, runID string) (*processEvidence, string) {
	t.Helper()
	data, err := board.ReadReviewOriginal(root, runID, board.ReviewProcessEvidence)
	if err != nil {
		t.Fatal(err)
	}
	var evidence processEvidence
	if err = json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	return &evidence, string(data)
}

// reviewerChild replaces the fake codex exit with a script body.
func reviewerChild(t *testing.T, h *reviewHarness, body string) {
	t.Helper()
	writeFake(t, h.fake, strings.Replace(fakeCodex, "exit 0\n", body+"\nexit 0\n", 1))
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		table, err := snapshotProcessTable(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info, ok := table[pid]; !ok || info.zombie {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d survived", pid)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestChildExitingDuringRecheckIsNotLeftover(t *testing.T) {
	h, root, args := archiveHarness(t)
	reviewerChild(t, h, "(sleep 0.2) >/dev/null 2>&1 &")
	code, out, stderr := captureRun(t, args)
	if code != 0 || !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	evidence, _ := archivedEvidence(t, root, "stable")
	if !evidence.Recheck.Performed || evidence.Cleanup.Sigkill != sigkillNotSent || !evidence.Cleanup.Collected {
		t.Fatalf("%+v", evidence)
	}
	if len(evidence.Members) == 0 {
		t.Fatal("the exiting child must be recorded")
	}
	for _, m := range evidence.Members {
		if m.Outcome != memberExited {
			t.Fatalf("%+v", m)
		}
	}
}

func TestLeftoverIsRejectedCollectedAndAttributed(t *testing.T) {
	h, root, args := archiveHarness(t)
	pidFile := filepath.Join(h.root, "child.pid")
	secret := "argv-secret-value"
	privateDir := filepath.Join(h.root, "private dir")
	if err := os.Mkdir(privateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(privateDir, "helper")
	writeFake(t, helper, "#!/bin/sh\nexec sleep 20\n")
	reviewerChild(t, h, fmt.Sprintf("%q %s >/dev/null 2>&1 &\nprintf '%%s\\n' \"$!\" > %q", helper, secret, pidFile))
	code, _, stderr := captureRun(t, args)
	location := "reviews/stable/" + board.ReviewProcessEvidence
	if code != 2 || !strings.Contains(stderr, "background child processes") || !strings.Contains(stderr, location) {
		t.Fatalf("%d %s", code, stderr)
	}
	pid := readPID(t, pidFile)
	waitProcessGone(t, pid)
	run, err := board.ReadReviewRun(root, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionStatus != "failed" || !strings.Contains(run.FailureReason, location) {
		t.Fatalf("%+v", run)
	}
	if err = board.ReviewPublicationComplete(root, "stable"); err != nil {
		t.Fatal(err)
	}
	evidence, raw := archivedEvidence(t, root, "stable")
	if !evidence.Recheck.Performed || evidence.Cleanup.Sigkill != sigkillSent || !evidence.Cleanup.Collected {
		t.Fatalf("%+v", evidence)
	}
	var found *processMember
	for i, m := range evidence.Members {
		if m.PID == pid {
			found = &evidence.Members[i]
		}
	}
	if found == nil || found.Outcome != memberLeftover || found.PGID != evidence.LeaderPID || found.PPID == 0 ||
		found.StartTime == "" || found.Executable != "sleep" || found.ObservedAt == "" {
		t.Fatalf("%+v in %s", found, raw)
	}
	for _, private := range []string{secret, h.root, "private dir"} {
		if strings.Contains(raw, private) {
			t.Fatalf("evidence leaks %q: %s", private, raw)
		}
	}
}

func TestStandaloneLeftoverPrintsEvidenceWithoutLocation(t *testing.T) {
	h := newCodexHarness(t)
	reviewerChild(t, h, "(sleep 20) >/dev/null 2>&1 &")
	code, _, stderr := h.defaultReview()
	if code != 2 || !strings.Contains(stderr, "background child processes") {
		t.Fatalf("%d %s", code, stderr)
	}
	line := ""
	for candidate := range strings.SplitSeq(stderr, "\n") {
		if strings.HasPrefix(candidate, "process evidence: {") {
			line = strings.TrimPrefix(candidate, "process evidence: ")
		}
	}
	var evidence processEvidence
	if err := json.Unmarshal([]byte(line), &evidence); err != nil {
		t.Fatalf("%v in %s", err, stderr)
	}
	if len(evidence.Members) == 0 || evidence.Cleanup.Sigkill != sigkillSent {
		t.Fatalf("%+v", evidence)
	}
	if strings.Contains(stderr, board.ReviewProcessEvidence) {
		t.Fatalf("no run, no location: %s", stderr)
	}
}

func TestSnapshotFailureRejectsAndRecordsTheFailure(t *testing.T) {
	h, root, args := archiveHarness(t)
	pidFile := filepath.Join(h.root, "child.pid")
	reviewerChild(t, h, fmt.Sprintf("(sleep 20) >/dev/null 2>&1 &\nprintf '%%s\\n' \"$!\" > %q", pidFile))
	injectRead(t, func(context.Context) (map[int]procInfo, error) { return nil, errors.New("snapshot refused") })
	code, _, stderr := captureRun(t, args)
	if code != 2 || !strings.Contains(stderr, "could not observe its processes") {
		t.Fatalf("%d %s", code, stderr)
	}
	evidence, _ := archivedEvidence(t, root, "stable")
	if !evidence.Observation.Failed || !strings.Contains(evidence.Observation.Reason, "snapshot refused") || evidence.Cleanup.Sigkill != sigkillSent {
		t.Fatalf("%+v", evidence)
	}
	waitProcessGone(t, readPID(t, pidFile))
}

func TestAbnormalEndsSkipTheRecheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		code   int
		signal bool
	}{
		{"failed exit", "#!/bin/sh\ncat >/dev/null\n(sleep 20) >/dev/null 2>&1 &\nexit 3\n", 3, false},
		{"timeout", "#!/bin/sh\ncat >/dev/null\nsleep 20\n", 124, false},
		{"interrupt", "#!/bin/sh\ncat >/dev/null\nprintf started > \"$FAKE_CODEX_ARGV\"\nsleep 20\n", 130, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, root, args := archiveHarness(t)
			writeFake(t, h.fake, tc.script)
			if tc.code == 124 {
				t.Setenv("CODEX_REVIEW_MAX_RUNTIME_SECONDS", "1")
			}
			if tc.signal {
				go func() {
					for range 500 {
						if _, err := os.Stat(h.argvLog); err == nil {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
				}()
			}
			code, _, stderr := captureRun(t, args)
			if code != tc.code {
				t.Fatalf("%d %s", code, stderr)
			}
			evidence, _ := archivedEvidence(t, root, "stable")
			if evidence.Recheck.Performed || evidence.Cleanup.Sigkill != sigkillSent || len(evidence.Members) == 0 {
				t.Fatalf("%+v", evidence)
			}
			for _, m := range evidence.Members {
				if m.Outcome != memberNotRechecked {
					t.Fatalf("%+v", m)
				}
			}
		})
	}
}

func TestOwnHelperIsRecordedButNotLeftover(t *testing.T) {
	h := newCursorHarness(t)
	t.Setenv("FAKE_CURSOR_OWN_CHILD", "1")
	code, _, stderr := h.review("cursor", "PMQA", "确认改动正确")
	if code != 0 || !strings.Contains(stderr, `"outcome":"attributed"`) {
		t.Fatalf("%d %s", code, stderr)
	}
}

func TestLateMemberIsLeftover(t *testing.T) {
	calls := injectTables(t,
		map[int]procInfo{100: member("a")},
		map[int]procInfo{100: member("a"), 200: member("b")},
		map[int]procInfo{200: member("b")},
	)
	collection, empty := observeReviewerGroup(fakeGroup(), true)
	got := outcomes(collection.evidence)
	if !collection.lingering || empty || got["100/a"] != memberExited || got["200/b"] != memberLeftover || *calls < 3 {
		t.Fatalf("%+v %v %d", collection, got, *calls)
	}
}

func TestReusedPIDInsideTheGroupIsLeftover(t *testing.T) {
	injectTables(t, map[int]procInfo{100: member("first")}, map[int]procInfo{100: member("second")})
	collection, _ := observeReviewerGroup(fakeGroup(), true)
	got := outcomes(collection.evidence)
	if !collection.lingering || got["100/first"] != memberExited || got["100/second"] != memberLeftover {
		t.Fatalf("%+v %v", collection, got)
	}
}

func TestReusedPIDOutsideTheGroupIsNotLeftover(t *testing.T) {
	stranger := member("second")
	stranger.pgid = 7
	injectTables(t, map[int]procInfo{100: member("first")}, map[int]procInfo{100: stranger})
	collection, empty := observeReviewerGroup(fakeGroup(), true)
	got := outcomes(collection.evidence)
	if collection.lingering || !empty || len(got) != 1 || got["100/first"] != memberExited {
		t.Fatalf("%+v %v", collection, got)
	}
}

func TestUnrelatedProcessesStayOutOfTheRecord(t *testing.T) {
	stranger := member("x")
	stranger.pgid = 7
	injectTables(t, map[int]procInfo{1: stranger, 2: stranger})
	collection, empty := observeReviewerGroup(fakeGroup(), true)
	if collection.lingering || !empty || len(collection.evidence.Members) != 0 {
		t.Fatalf("%+v", collection)
	}
}

func TestZombieMemberIsNotLeftover(t *testing.T) {
	dead := member("a")
	dead.zombie = true
	injectTables(t, map[int]procInfo{100: dead})
	collection, empty := observeReviewerGroup(fakeGroup(), true)
	if collection.lingering || !empty {
		t.Fatalf("%+v", collection)
	}
}

func TestTrackerAttribution(t *testing.T) {
	table := map[int]procInfo{100: member("own"), 200: member("stray")}
	injectTables(t, table)
	tracked := fakeGroup()
	tracked.tracker = newReviewerProcessTree(leaderPID)
	tracked.tracker.ownTree[100] = struct{}{}
	tracked.tracker.observed = true
	collection, empty := observeReviewerGroup(tracked, true)
	got := outcomes(collection.evidence)
	if !collection.lingering || empty || got["100/own"] != memberAttributed || got["200/stray"] != memberLeftover {
		t.Fatalf("%+v %v", collection, got)
	}

	delete(table, 200)
	collection, empty = observeReviewerGroup(tracked, true)
	if collection.lingering || empty || collection.evidence.Recheck.DurationMS >= recheckPollInterval.Milliseconds() {
		t.Fatalf("attributed member must end the recheck at once: %+v", collection)
	}

	// A tracker that never finished an observation attributes nothing.
	tracked.tracker.observed = false
	collection, _ = observeReviewerGroup(tracked, true)
	if !collection.lingering || outcomes(collection.evidence)["100/own"] != memberLeftover {
		t.Fatalf("%+v", collection)
	}
}

func TestRecheckStaysInsideItsLimit(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	for _, tc := range []struct {
		name   string
		read   func(context.Context) (map[int]procInfo, error)
		failed bool
	}{
		{"hung snapshot", func(context.Context) (map[int]procInfo, error) { <-block; return nil, nil }, true},
		{"slow snapshot", func(context.Context) (map[int]procInfo, error) {
			time.Sleep(recheckSnapshotLimit - 100*time.Millisecond)
			return map[int]procInfo{100: member("a")}, nil
		}, false},
		{"fast snapshot", func(context.Context) (map[int]procInfo, error) {
			return map[int]procInfo{100: member("a")}, nil
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			injectRead(t, tc.read)
			started := time.Now()
			collection, _ := observeReviewerGroup(fakeGroup(), true)
			if elapsed := time.Since(started); elapsed > reviewerRecheckLimit {
				t.Fatalf("recheck took %v", elapsed)
			}
			if !collection.lingering || collection.evidence.Observation.Failed != tc.failed {
				t.Fatalf("%+v", collection)
			}
		})
	}
}

func TestMemberRecordsAreCappedWithLeftoversFirst(t *testing.T) {
	first := map[int]procInfo{}
	for pid := 1; pid <= processEvidenceMaxMembers+36; pid++ {
		first[pid] = member("a")
	}
	last := processEvidenceMaxMembers + 1000
	injectTables(t, first, map[int]procInfo{last: member("b")})
	collection, _ := observeReviewerGroup(fakeGroup(), true)
	e := collection.evidence
	if len(e.Members) != processEvidenceMaxMembers || e.OmittedMembers != 37 || e.Members[0].PID != last || e.Members[0].Outcome != memberLeftover {
		t.Fatalf("%d %d %+v", len(e.Members), e.OmittedMembers, e.Members[0])
	}
}

func TestParseProcStat(t *testing.T) {
	tail := " S 10 20 20 0 -1 4194304 1 2 3 4 5 6 7 8 20 0 1 0 987654 12345 67"
	for _, tc := range []struct {
		record, executable string
	}{
		{"42 (sleep)" + tail, "sleep"},
		{"42 (my tool (x))" + tail, "my tool (x)"},
		{"42 (dir/name)" + tail, "name"},
	} {
		info, ok := parseProcStat(tc.record)
		if !ok || info.ppid != 10 || info.pgid != 20 || info.zombie || info.startTime != "987654" || info.executable != tc.executable {
			t.Fatalf("%q: %+v %v", tc.record, info, ok)
		}
	}
	if info, ok := parseProcStat("42 (sleep) Z 10 20"); !ok || !info.zombie || info.startTime != "" {
		t.Fatalf("%+v %v", info, ok)
	}
	if _, ok := parseProcStat("garbage"); ok {
		t.Fatal("garbage accepted")
	}
}

func TestParsePsLine(t *testing.T) {
	const start = "Sat Nov  1 04:05:06 2026"
	for _, tc := range []struct {
		line string
		pid  int
		info procInfo
	}{
		{"  501   1 501 S    " + start + " /usr/bin/ssh", 501, procInfo{ppid: 1, pgid: 501, startTime: "Sat Nov 1 04:05:06 2026", executable: "ssh"}},
		{"  77  12  77 Ss+  " + start + " /Applications/My App.app/Contents/MacOS/My App Helper", 77, procInfo{ppid: 12, pgid: 77, startTime: "Sat Nov 1 04:05:06 2026", executable: "My App Helper"}},
		{"  78  12  77 R    " + start + " node", 78, procInfo{ppid: 12, pgid: 77, startTime: "Sat Nov 1 04:05:06 2026", executable: "node"}},
		{"  79  12  77 Z    -", 79, procInfo{ppid: 12, pgid: 77, zombie: true}},
	} {
		pid, info, ok := parsePsLine(tc.line)
		if !ok || pid != tc.pid || info != tc.info {
			t.Fatalf("%q: %d %+v %v", tc.line, pid, info, ok)
		}
	}
	for _, line := range []string{"", "ps: warning", "a b c d", "1 2"} {
		if _, _, ok := parsePsLine(line); ok {
			t.Fatalf("%q accepted", line)
		}
	}
}

func TestProcessTableHasIdentity(t *testing.T) {
	table, err := snapshotProcessTable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	self, ok := table[os.Getpid()]
	if !ok || self.startTime == "" || self.executable == "" || strings.Contains(self.executable, "/") {
		t.Fatalf("%+v %v", self, ok)
	}
}
