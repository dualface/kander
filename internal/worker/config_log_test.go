package worker

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
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

// A configuration that cannot be loaded is logged and the worker goes on.
func TestWorkerGoesOnWhenTheConfigurationCannotBeLoaded(t *testing.T) {
	path := writeWorkerConfig(t, func(map[string]any) {})
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, buf := bufferLogger()
	logIgnoredConfig(logger)
	records := logRecords(t, buf)
	if len(records) != 1 || records[0]["level"] != "WARN" {
		t.Fatalf("log=%s", buf.String())
	}
}
