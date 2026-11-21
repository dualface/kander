package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/server"
)

const ignoredLogMessage = "the configuration holds a setting this version ignores"

// withFutureKeys adds content of a newer version to the scope configuration that
// serverCommandEnv saved: an unknown key and an unknown key inside the server section.
func withFutureKeys(t *testing.T) {
	t.Helper()
	path := os.Getenv(config.EnvConfig)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["future_top"] = map[string]any{"secret": "value-from-a-newer-version"}
	config.OverlaySet(doc, "secret-token", "server", "token")
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The log lines of a text, one parsed object per line that names the ignored-setting message.
func ignoredLogLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.Contains(line, ignoredLogMessage) {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		lines = append(lines, record)
	}
	return lines
}

func requireIgnoredWarnings(t *testing.T, lines []map[string]any) {
	t.Helper()
	got := map[string]bool{}
	for _, line := range lines {
		if line["level"] != "WARN" {
			t.Fatalf("level=%v", line["level"])
		}
		got[line["path"].(string)] = true
	}
	if len(lines) != 2 || !got["future_top"] || !got["server.token"] {
		t.Fatalf("warnings=%v", lines)
	}
}

// A foreground server logs each ignored setting once, by path and kind, after its logger exists
// and before it serves. Without --json the lines go to stderr.
func TestForegroundServerLogsIgnoredConfiguration(t *testing.T) {
	serverCommandEnv(t)
	withFutureKeys(t)
	original := runForegroundServer
	defer func() { runForegroundServer = original }()
	runForegroundServer = func(context.Context, server.Options, string) error { return nil }
	r := runServerJSON(t, "start", "--foreground", "--data", t.TempDir())
	if r.code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.code, r.stderr)
	}
	requireIgnoredWarnings(t, ignoredLogLines(t, r.stderr))
	for _, secret := range []string{"value-from-a-newer-version", "secret-token"} {
		if strings.Contains(r.stderr+r.stdout, secret) {
			t.Fatalf("a value reached the output: %s", r.stderr)
		}
	}
}

// With --json the lines written before the server can serve are server_log notices, as every
// other line of that phase.
func TestForegroundServerJSONLogsIgnoredConfigurationAsNotices(t *testing.T) {
	serverCommandEnv(t)
	withFutureKeys(t)
	original := runForegroundServer
	defer func() { runForegroundServer = original }()
	runForegroundServer = func(_ context.Context, o server.Options, _ string) error {
		o.Ready("127.0.0.1:4242")
		return nil
	}
	r := runServerJSON(t, "start", "--json", "--foreground", "--data", t.TempDir())
	if r.code != 0 || jsonLine(t, r.stdout)["listen"] != "127.0.0.1:4242" {
		t.Fatalf("exit=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	// The notices of the phase before the first line of stdout follow it on stderr, one
	// {"notice":...} line each.
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(r.stderr), "\n") {
		var object struct {
			Notice struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"notice"`
		}
		if err := json.Unmarshal([]byte(line), &object); err != nil || object.Notice.Code != "server_log" {
			t.Fatalf("stderr line is not a server_log notice: %q", line)
		}
		lines = append(lines, ignoredLogLines(t, object.Notice.Message)...)
	}
	requireIgnoredWarnings(t, lines)
}

func TestForegroundServerLogsNothingWithoutIgnoredConfiguration(t *testing.T) {
	serverCommandEnv(t)
	original := runForegroundServer
	defer func() { runForegroundServer = original }()
	runForegroundServer = func(context.Context, server.Options, string) error { return nil }
	r := runServerJSON(t, "start", "--foreground", "--data", t.TempDir())
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("exit=%d stderr=%q", r.code, r.stderr)
	}
}

// The background start runs the foreground path in its child: the parent writes nothing, and the
// child logs each ignored setting once into the server log.
func TestBackgroundServerStartLogsIgnoredConfigurationOnlyInTheChild(t *testing.T) {
	ctl, address, target := serverCommandEnv(t)
	withFutureKeys(t)
	dir := t.TempDir()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ctl.Stop(ctx, target)
	})
	r := runServerJSON(t, "start", "--data", dir, "--listen", address)
	if r.code != 0 || strings.Contains(r.stderr, ignoredLogMessage) || strings.Contains(r.stdout, ignoredLogMessage) {
		t.Fatalf("the parent wrote the warnings: exit=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	log, err := os.ReadFile(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	requireIgnoredWarnings(t, ignoredLogLines(t, string(log)))
	if bytes.Count(log, []byte(ignoredLogMessage)) != 2 {
		t.Fatalf("server.log=%s", log)
	}
}
