package liveness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func batchInputs(count int) []TaskInput {
	inputs := make([]TaskInput, count)
	for i := range inputs {
		id := strconv.Itoa(i)
		inputs[i] = TaskInput{Entry: board.Entry{TaskID: id}, Text: "- SESSION: codex wanted\n- WINDOW: herdr:w1:t1:w1:p" + id + "\n- OWNER: codex\n- STARTED_AT: 2026-09-08 02:07\n"}
	}
	return inputs
}

// The event log measures actual external command concurrency, including slow
// commands that are terminated before they can append an end event.
func installBatchFake(t *testing.T, fast bool) string {
	t.Helper()
	installPOSIXFakes(t, true)
	dir := t.TempDir()
	t.Setenv("BATCH_EVENTS", filepath.Join(dir, "events"))
	t.Setenv("BATCH_PIDS", dir)
	script := `#!/bin/sh
printf 'start %s\n' "$3" >> "$BATCH_EVENTS"
echo $$ > "$BATCH_PIDS/$3"
`
	if fast {
		script += `if [ "$3" = w1:p0 ]; then
 /bin/sleep 0.05
 printf 'end %s\n' "$3" >> "$BATCH_EVENTS"
 echo '{"result":{"pane":{"pane_id":"w1:p0","agent":"codex","agent_status":"blocked","agent_session":{"value":"wanted"}}}}'
 exit 0
fi
`
	}
	script += "exec /bin/sleep 3\n"
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertBatchPeakAndCleanup(t *testing.T, dir string, wantPeak, wantStarts int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "events"))
	if err != nil {
		t.Fatal(err)
	}
	active, peak, starts := 0, 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "start ") {
			active++
			starts++
			peak = max(peak, active)
		} else {
			active--
		}
	}
	if peak != wantPeak || starts != wantStarts {
		t.Fatalf("peak=%d starts=%d; want %d/%d; events=%s", peak, starts, wantPeak, wantStarts, data)
	}
	if runtime.GOOS != "linux" {
		t.Log("process disappearance assertion requires Linux /proc; command concurrency was checked")
		return
	}
	files, err := filepath.Glob(filepath.Join(dir, "w1:p*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		pid, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := strconv.Atoi(strings.TrimSpace(string(pid))); err != nil || value <= 0 {
			t.Fatalf("invalid probe PID record %q: %v", pid, err)
		}
		if _, err := os.Stat(filepath.Join("/proc", strings.TrimSpace(string(pid)))); !os.IsNotExist(err) {
			t.Fatalf("owned probe process %s remains: %v", pid, err)
		}
	}
}

func TestBatchMixedCommandsShareTotalBudget(t *testing.T) {
	resetLang(t)
	dir := installBatchFake(t, true)
	inputs := batchInputs(12)
	started := time.Now()
	reports := ClassifyTasksContext(context.Background(), inputs, BatchOptions{Budget: 400 * time.Millisecond, Concurrency: 2})
	if elapsed := time.Since(started); elapsed < 400*time.Millisecond || elapsed > 750*time.Millisecond {
		t.Fatalf("batch elapsed=%s; 12 tasks share 400ms", elapsed)
	}
	if len(reports) != len(inputs) {
		t.Fatalf("reports=%d", len(reports))
	}
	for i, rep := range reports {
		if rep.TaskID != inputs[i].Entry.TaskID || rep.Identity != identityFrom(inputs[i].Entry, inputs[i].Text) {
			t.Fatalf("input order/identity lost: %+v", rep)
		}
		if i == 0 {
			if rep.Status != Alive || rep.RuntimeState != "blocked" || !rep.ValidFor(inputs[i].Entry, inputs[i].Text) || rep.ObservedAt.Before(started) || rep.ObservedAt.After(time.Now()) {
				t.Fatalf("completed observation lost: %+v", rep)
			}
		} else if rep.Status != Unknown || rep.ObservationValid || rep.NewWindow != "" || !strings.Contains(rep.Detail, "探测期限已耗尽") {
			t.Fatalf("expired observation=%+v", rep)
		}
		if i >= 3 && !rep.ObservedAt.IsZero() {
			t.Fatalf("unstarted task has observation time: %+v", rep)
		}
	}
	assertBatchPeakAndCleanup(t, dir, 2, 3)
}

func TestBatchDefaultConcurrencyCancellationJoinsWorkers(t *testing.T) {
	resetLang(t)
	dir := installBatchFake(t, false)
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan []Report, 1)
	go func() { done <- ClassifyTasksContext(ctx, batchInputs(30), BatchOptions{}) }()
	ready := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		files, _ := filepath.Glob(filepath.Join(dir, "w1:p*"))
		if len(files) == DefaultBatchConcurrency {
			ready = true
			for _, file := range files {
				data, err := os.ReadFile(file)
				pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil || parseErr != nil || pid <= 0 {
					ready = false
					break
				}
			}
			if ready {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	cancel()
	reports := <-done
	if !ready || time.Since(started) > 300*time.Millisecond {
		t.Fatalf("ready=%v cancellation took %s", ready, time.Since(started))
	}
	for _, rep := range reports {
		if rep.Status != Unknown || rep.ObservationValid || !strings.Contains(rep.Detail, "探测已取消") {
			t.Fatalf("report=%+v", rep)
		}
	}
	assertBatchPeakAndCleanup(t, dir, DefaultBatchConcurrency, DefaultBatchConcurrency)
	for deadline := time.Now().Add(300 * time.Millisecond); runtime.NumGoroutine() > baseline && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline {
		t.Fatalf("goroutines before=%d after=%d", baseline, n)
	}
}

func TestBatchEarlierCallerDeadlineWins(t *testing.T) {
	resetLang(t)
	dir := installBatchFake(t, false)
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	reports := ClassifyTasksContext(ctx, batchInputs(8), BatchOptions{Budget: time.Second, Concurrency: 2})
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("caller deadline ignored: %s", elapsed)
	}
	for _, rep := range reports {
		if rep.Status != Unknown || rep.ObservationValid {
			t.Fatalf("report=%+v", rep)
		}
	}
	assertBatchPeakAndCleanup(t, dir, 2, 2)
}

func TestBatchUncollectedAndReadErrors(t *testing.T) {
	resetLang(t)
	if reports := ClassifyTasksContext(context.Background(), nil, BatchOptions{}); len(reports) != 0 {
		t.Fatal(reports)
	}
	inputs := batchInputs(1)
	inputs[0].ReadError = errors.New("document unavailable")
	rep := ClassifyTasksContext(context.Background(), inputs, BatchOptions{Concurrency: 100})[0]
	if rep.Status != Unknown || !rep.ObservedAt.IsZero() || rep.ValidFor(inputs[0].Entry, inputs[0].Text) || !strings.Contains(rep.Detail, "document unavailable") {
		t.Fatalf("read failure=%+v", rep)
	}
	inputs[0].ReadError = nil
	for lang, want := range map[string]string{"cn": "批量任务未采集", "en": "Batch task not observed", "ja": "バッチ内のタスクは未観測です"} {
		config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rep = ClassifyTasksContext(ctx, inputs, BatchOptions{})[0]
		if rep.Status != Unknown || !rep.ObservedAt.IsZero() || !strings.Contains(rep.Detail, want) {
			t.Fatalf("lang=%s report=%+v", lang, rep)
		}
		line := formatReport(rep)
		if strings.Contains(line, "liveness.observation") || !strings.Contains(line, "false") || !strings.Contains(line, Unknown) {
			t.Fatalf("lang=%s line=%s", lang, line)
		}
	}
}

func TestCheckCollectsBatchAndPreservesOutputOrder(t *testing.T) {
	root := tempBoard(t)
	installPOSIXFakes(t, true)
	dir := t.TempDir()
	t.Setenv("BATCH_EVENTS", filepath.Join(dir, "events"))
	script := `#!/bin/sh
printf 'start %s\n' "$3" >> "$BATCH_EVENTS"
/bin/sleep 0.1
printf 'end %s\n' "$3" >> "$BATCH_EVENTS"
printf '{"result":{"pane":{"pane_id":"%s","agent":"codex","agent_status":"idle","agent_session":{"value":"wanted"}}}}\n' "$3"
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 4; i++ {
		id, path := makeWorking(t, "batch-check-"+strconv.Itoa(i), "批量采集")
		setLocation(t, path, "codex wanted", "herdr:w1:t1:w1:p"+strconv.Itoa(i))
		ids = append(ids, id)
	}
	lines, err := livenessLines(root, nil)
	if err != nil || len(lines) != len(ids) {
		t.Fatalf("lines=%v err=%v", lines, err)
	}
	for i, line := range lines {
		if !strings.Contains(line, ids[i]) || !strings.Contains(line, "alive") || !strings.Contains(line, "idle") || !strings.Contains(line, "true") {
			t.Fatalf("line %d=%s", i, line)
		}
	}
	assertBatchPeakAndCleanup(t, dir, 4, 4)
}

func TestObservationRejectsNewIdentityAndKeepsRuntimeSeparate(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	input := batchInputs(1)[0]
	for _, state := range []string{"idle", "working", "blocked", "done"} {
		script := "#!/bin/sh\necho '{\"result\":{\"pane\":{\"pane_id\":\"w1:p0\",\"agent\":\"codex\",\"agent_status\":\"" + state + "\",\"agent_session\":{\"value\":\"wanted\"}}}}'\n"
		if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		rep := ClassifyTasksContext(context.Background(), []TaskInput{input}, BatchOptions{})[0]
		if rep.Status != Alive || rep.RuntimeState != state || !rep.ValidFor(input.Entry, input.Text) {
			t.Fatalf("state conflated with liveness: %+v", rep)
		}
		for _, replacement := range [][2]string{{"codex wanted", "codex replacement"}, {"codex wanted", "claude wanted"}, {"w1:p0", "w1:p9"}, {"OWNER: codex", "OWNER: claude"}, {"02:07", "02:08"}} {
			if rep.ValidFor(input.Entry, strings.ReplaceAll(input.Text, replacement[0], replacement[1])) {
				t.Fatalf("old observation accepted for changed %q", replacement[0])
			}
		}
		if rep.ValidFor(board.Entry{TaskID: "other"}, input.Text) {
			t.Fatal("old observation accepted for another task")
		}
		data, err := json.Marshal(rep)
		if err != nil || !strings.Contains(string(data), `"observed_at":`) || !strings.Contains(string(data), `"runtime_state":"`+state+`"`) {
			t.Fatalf("JSON=%s err=%v", data, err)
		}
	}
}

func TestBatchPreservesDriftAndTmuxRuntimeUnknown(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p0")
	t.Setenv("KANBAN_HERDR_SESSION", "wanted")
	t.Setenv("KANBAN_HERDR_STATUS", "done")
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "wanted")
	inputs := batchInputs(2)
	inputs[1].Text = "- SESSION: codex wanted\n- WINDOW: tmux:$1:@1:%1\n"
	reports := ClassifyTasksContext(context.Background(), inputs, BatchOptions{})
	if rep := reports[0]; rep.Status != Drifted || rep.NewWindow != "herdr:w1:t9:w1:p9" || rep.RuntimeState != "done" || !rep.ValidFor(inputs[0].Entry, inputs[0].Text) {
		t.Fatalf("drift observation=%+v", rep)
	}
	if rep := reports[1]; rep.Status != Alive || rep.RuntimeState != Unknown || !rep.ObservationValid {
		t.Fatalf("tmux presence is not an agent runtime state: %+v", rep)
	}
}
