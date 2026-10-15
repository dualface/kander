package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

// codexScanFixture returns the sessions root and launch directory of a fresh board.
func codexScanFixture(t *testing.T) (root, cwd string) {
	t.Helper()
	board, _, _ := setupBoard(t)
	root, err := filepath.Abs(codexSessionsRoot())
	if err != nil {
		t.Fatal(err)
	}
	return root, filepath.Dir(board)
}

func TestCodexSnapshotReadsNoRollout(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions enforced for the current user")
	}
	codexScanFixture(t)
	unreadable := codexRolloutAt(t, "task", "unreadable", "unreadable-session", "", time.Now())
	if err := os.Chmod(unreadable.Path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable.Path, 0o600) })
	def := config.AgentFor(config.DefaultConfig(), "codex").Session
	previous, err := sessionDiscoverSnapshot(def, "task", true, nil, t.TempDir(), LaunchPlan{}.launchTimeout)
	if err != nil || len(previous) != 0 {
		t.Fatalf("previous=%v err=%v", previous, err)
	}
}

func TestCodexScanSkipsSessionsCreatedBeforeLaunch(t *testing.T) {
	root, cwd := codexScanFixture(t)
	const task = "task-1"
	launchStart := time.Now()
	// The same card's earlier session keeps writing during the launch: a fresh
	// mtime must not make it a candidate.
	old := codexRolloutAt(t, task, "running", "running-session", "", launchStart.Add(-time.Minute))
	appendRolloutPrompt(t, old.Path, codexPromptPrefixes(task)[0]+" again")
	undated := codexRolloutAt(t, task, "undated", "undated-session", "", launchStart.Add(time.Second))
	setRolloutTimestamp(t, undated.Path, nil)
	invalid := codexRolloutAt(t, task, "invalid-time", "invalid-time-session", "", launchStart.Add(time.Second))
	setRolloutTimestamp(t, invalid.Path, "not a time")
	scan := newCodexRolloutScan(root, codexPromptPrefixes(task), launchStart, cwd)
	got, err := scan.candidates(context.Background(), launchStart.Add(time.Second))
	if err != nil || len(got) != 0 {
		t.Fatalf("candidates=%v err=%v", got, err)
	}
	opened := scan.opened
	codexRolloutAt(t, task, "new", "new-session", "", launchStart.Add(time.Second))
	got, err = scan.candidates(context.Background(), launchStart.Add(2*time.Second))
	if err != nil || len(got) != 1 || got[0] != "new-session" {
		t.Fatalf("candidates=%v err=%v", got, err)
	}
	if scan.opened != opened+1 {
		t.Fatalf("settled rollouts reopened: opened %d then %d", opened, scan.opened)
	}
}

func TestCodexScanToleratesMissingDateDirectories(t *testing.T) {
	root, cwd := codexScanFixture(t)
	scan := newCodexRolloutScan(root, codexPromptPrefixes("task"), time.Now(), cwd)
	got, err := scan.candidates(context.Background(), time.Now())
	if err != nil || len(got) != 0 {
		t.Fatalf("candidates=%v err=%v", got, err)
	}
}

func TestCodexScanIgnoresCodexTimeZone(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("UTC+9", 9*60*60)
	t.Cleanup(func() { time.Local = previous })
	root, cwd := codexScanFixture(t)
	const task = "task-1"
	// The launch sits just before UTC midnight, so the new session lands in the
	// next UTC date directory.
	launchStart := time.Date(2026, 10, 13, 23, 59, 58, 0, time.UTC)
	created := launchStart.Add(3 * time.Second)
	for _, offset := range []int{-10, 0, 14} {
		zone := time.FixedZone("codex", offset*60*60)
		name := fmt.Sprintf("%s-tz%+d", created.In(zone).Format(codexRolloutNameLayout), offset)
		id := fmt.Sprintf("tz%d-session", offset+12) // ids allow no "+"
		file := codexRolloutAt(t, task, name, id, "", created)
		// Codex files the rollout under the date of its own time zone.
		local := filepath.Join(root, filepath.FromSlash(created.In(zone).Format("2006/01/02")))
		if err := os.MkdirAll(local, 0o700); err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(local, filepath.Base(file.Path))
		if err := os.Rename(file.Path, moved); err != nil {
			t.Fatal(err)
		}
		scan := newCodexRolloutScan(root, codexPromptPrefixes(task), launchStart, cwd)
		got, err := scan.candidates(context.Background(), created)
		if err != nil || len(got) != 1 || got[0] != id {
			t.Fatalf("offset %+d: candidates=%v err=%v", offset, got, err)
		}
		if err := os.Remove(moved); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexScanOpensNoOldRollout(t *testing.T) {
	root, cwd := codexScanFixture(t)
	const task = "task-1"
	launchStart := time.Now()
	old := launchStart.AddDate(0, -3, 0)
	for index := range 2000 {
		created := old.Add(time.Duration(index) * time.Hour)
		codexRolloutAt(t, task, fmt.Sprintf("%s-old-%d", created.UTC().Format(codexRolloutNameLayout), index), fmt.Sprintf("old-%d", index), "", created)
	}
	// Named within the candidate days but more than the name skew before launch.
	stale := launchStart.Add(-codexRolloutNameSkew - time.Hour)
	codexRolloutAt(t, task, stale.UTC().Format(codexRolloutNameLayout)+"-stale", "stale-session", "", stale)
	codexRolloutAt(t, task, launchStart.UTC().Format(codexRolloutNameLayout)+"-new", "new-session", "", launchStart.Add(time.Second))
	scan := newCodexRolloutScan(root, codexPromptPrefixes(task), launchStart, cwd)
	got, err := scan.candidates(context.Background(), launchStart.Add(time.Second))
	if err != nil || len(got) != 1 || got[0] != "new-session" {
		t.Fatalf("candidates=%v err=%v", got, err)
	}
	if scan.opened != 1 {
		t.Fatalf("opened %d rollouts, want only the new one", scan.opened)
	}
}

func TestCodexPromptPrefixesBuiltOncePerLookup(t *testing.T) {
	_, cwd := codexScanFixture(t)
	const task = "task-1"
	for index := range 5 {
		repairRollout(t, task, fmt.Sprintf("old-%d", index), fmt.Sprintf("old-%d", index), "")
	}
	calls := 0
	original := currentInstallPaths
	currentInstallPaths = func() (config.InstallPaths, error) {
		calls++
		return original()
	}
	t.Cleanup(func() { currentInstallPaths = original })
	count := func(name string, run func()) {
		t.Helper()
		calls = 0
		run()
		if calls != 1 {
			t.Fatalf("%s built the prompt prefixes %d times", name, calls)
		}
	}
	count("discovery", func() {
		freezeClock(t)
		// Every poll until the deadline finds no candidate.
		if _, err := discoverNewCodexSession(task, nowFn(), cwd, sessionDiscoverWait); err == nil {
			t.Fatal("discovery without a new session succeeded")
		}
	})
	count("lookup", func() {
		if _, err := codexSessionsForTask(task); err != nil {
			t.Fatal(err)
		}
	})
	var files process.ProcessFiles
	files.Identity = "pid"
	for index := range 3 {
		files.Files = append(files.Files, codexRolloutAt(t, task, fmt.Sprintf("open-%d", index), fmt.Sprintf("open-%d", index), "unrelated", time.Now()))
	}
	count("proof", func() {
		_, _ = proveOpenSession(context.Background(), files, task, "", cwd)
	})
}

func appendRolloutPrompt(t *testing.T, path, prompt string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	record := map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": prompt}}
	if err := json.NewEncoder(file).Encode(record); err != nil {
		t.Fatal(err)
	}
}

// setRolloutTimestamp rewrites the session_meta payload timestamp; nil removes it.
func setRolloutTimestamp(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	payload := records[0]["payload"].(map[string]any)
	if value == nil {
		delete(payload, "timestamp")
	} else {
		payload["timestamp"] = value
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, record := range records {
		if err := json.NewEncoder(file).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
}
