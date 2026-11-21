package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		records = append(records, record)
	}
	return records
}

func writeWorkerConfig(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, path)
	t.Chdir(t.TempDir())
	data, err := json.Marshal(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	mutate(doc)
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A worker that starts with a configuration holding settings of a newer version logs each of
// them once as a warning, by path and kind and never by value.
func TestWorkerLogsIgnoredConfigurationOnce(t *testing.T) {
	writeWorkerConfig(t, func(doc map[string]any) {
		doc["future_top"] = map[string]any{"secret": "value-from-a-newer-version"}
		doc["server"] = map[string]any{"token": "secret-token"}
		doc["agents"] = map[string]any{"badagent": map[string]any{"path": "sh", "dialect": "futuredialect"}}
	})
	logger, buf := bufferLogger()
	logIgnoredConfig(logger)
	records := logRecords(t, buf)
	got := map[string]string{}
	for _, record := range records {
		if record["level"] != "WARN" {
			t.Fatalf("level=%v", record["level"])
		}
		path, _ := record["path"].(string)
		kind, _ := record["kind"].(string)
		if _, dup := got[path]; dup {
			t.Fatalf("%s logged twice", path)
		}
		got[path] = kind
	}
	want := map[string]string{"future_top": config.IgnoredField, "server.token": config.IgnoredField, "agents.badagent": config.IgnoredUnusableAgent}
	if len(got) != len(want) {
		t.Fatalf("logged %v, want %v", got, want)
	}
	for path, kind := range want {
		if got[path] != kind {
			t.Fatalf("logged %v, want %v", got, want)
		}
	}
	for _, secret := range []string{"value-from-a-newer-version", "secret-token", "futuredialect"} {
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("a value reached the log: %s", buf.String())
		}
	}
}

func TestWorkerLogsNothingWithoutIgnoredConfiguration(t *testing.T) {
	writeWorkerConfig(t, func(map[string]any) {})
	logger, buf := bufferLogger()
	logIgnoredConfig(logger)
	if buf.Len() != 0 {
		t.Fatalf("log=%s", buf.String())
	}
}

// The worker runs in the client home, which may sit below a directory that holds a project
// overlay. Its startup report covers the scope configuration only: the overlay's own ignored
// settings are not logged, and an overlay that does not load does not hide the scope's.
func TestWorkerLogsOnlyTheScopeConfigurationBelowAProjectOverlay(t *testing.T) {
	writeWorkerConfig(t, func(doc map[string]any) { doc["future_top"] = true })
	project := t.TempDir()
	home := filepath.Join(project, "client")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(home)
	overlay := filepath.Join(project, config.OverlayFilename)
	for name, content := range map[string]string{
		"an overlay with ignored keys": `{"future_overlay":{"x":1},"server":{"token":"overlay-secret"}}`,
		"an overlay that is invalid":   `{"tui":"not an object"}`,
	} {
		if err := os.WriteFile(overlay, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		logger, buf := bufferLogger()
		logIgnoredConfig(logger)
		records := logRecords(t, buf)
		if len(records) != 1 || records[0]["path"] != "future_top" {
			t.Fatalf("%s: log=%s", name, buf.String())
		}
	}
}

// A worker that starts with a configuration that cannot be loaded logs one warning and goes on:
// it takes the worker lock and writes its record, and it ends with status 0 when told to stop.
func TestWorkerStartsWhenTheConfigurationCannotBeLoaded(t *testing.T) {
	path := writeWorkerConfig(t, func(map[string]any) {})
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "client")
	var log syncBuffer
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	status := make(chan int, 1)
	go func() { status <- runWorker(ctx, home, &log) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, state, _ := remote.ReadWorkerRecord(home); state == remote.WorkerRecordRecorded {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("the worker did not start: exit=%v log=%s", <-status, log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	if code := <-status; code != 0 {
		t.Fatalf("exit=%d log=%s", code, log.String())
	}
	buf := bytes.NewBufferString(log.String())
	var warnings int
	for _, record := range logRecords(t, buf) {
		if record["level"] == "WARN" && strings.Contains(record["msg"].(string), "could not be loaded") {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("warnings=%d log=%s", warnings, log.String())
	}
}

// syncBuffer is a log destination that the worker goroutine writes while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
