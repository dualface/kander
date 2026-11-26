package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/server"
)

type remarkLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *remarkLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}

type loggedRemark struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Code  string `json:"code"`
	Text  string `json:"text"`
}

func (l *remarkLog) remarks(t *testing.T) []loggedRemark {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var records []loggedRemark
	for line := range strings.SplitSeq(l.buffer.String(), "\n") {
		if line == "" {
			continue
		}
		var r loggedRemark
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		if r.Msg == "board remark" {
			records = append(records, r)
		}
	}
	return records
}

func TestEmbeddedBoardRemarksAreScopedToTheInstance(t *testing.T) {
	var fallback []string
	old := board.Remark
	board.Remark = func(code, _ string) { fallback = append(fallback, code) }
	t.Cleanup(func() { board.Remark = old })

	partition := loggedRemark{"INFO", "board remark", "board_journal_partitioned",
		strings.TrimRight(config.Text("board.journal_partitioned", "0"), "\r\n")}
	warning := loggedRemark{"WARN", "board remark", "board_warning",
		strings.TrimRight(config.Text("board.kander_warning_ignored_invalid_entries_run_kander_check_for", "1"), "\r\n")}
	aLog, bLog := &remarkLog{}, &remarkLog{}
	aOpts := server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewJSONHandler(aLog, nil))}
	a := openEmbedded(t, aOpts)
	b := openEmbedded(t, server.Options{Logger: slog.New(slog.NewJSONHandler(bLog, nil))})
	const registration = `{"name":"logged"}`
	p := request[api.Project](t, a.Handler(), http.MethodPost, "/api/v1/projects", registration, http.StatusCreated)
	if got := aLog.remarks(t); !slices.Equal(got, []loggedRemark{partition}) || len(bLog.remarks(t)) != 0 {
		t.Fatalf("first registration leaked or lost a remark: first %v, second %v", got, bLog.remarks(t))
	}
	q := request[api.Project](t, b.Handler(), http.MethodPost, "/api/v1/projects", registration, http.StatusCreated)
	if p.ID != q.ID || p.BoardPath == q.BoardPath {
		t.Fatalf("expected the same project ID in two boards: %+v and %+v", p, q)
	}
	if err := os.WriteFile(filepath.Join(p.BoardPath, "backlog", "stray.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + p.ID + "/tasks/20261126-missing-task/move"
	request[api.Error](t, a.Handler(), http.MethodPost, path, `{"state":"todo"}`, http.StatusNotFound)
	if aGot, bGot := aLog.remarks(t), bLog.remarks(t); !slices.Equal(aGot, []loggedRemark{partition, warning}) ||
		!slices.Equal(bGot, []loggedRemark{partition}) || len(fallback) != 0 {
		t.Fatalf("misrouted request remarks: first %v, second %v, fallback %v", aGot, bGot, fallback)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := board.RecoverTransactions(p.BoardPath); err != nil {
		t.Fatal(err)
	}
	if got := aLog.remarks(t); !slices.Equal(got, []loggedRemark{partition, warning}) || !slices.Equal(fallback, []string{partition.Code}) {
		t.Fatalf("closed project still intercepted remarks: log %v, fallback %v", got, fallback)
	}
	openEmbedded(t, aOpts)
	if got := aLog.remarks(t); !slices.Equal(got, []loggedRemark{partition, warning, partition, warning}) ||
		!slices.Equal(fallback, []string{partition.Code}) {
		t.Fatalf("opening an existing project did not route its remarks: log %v, fallback %v", got, fallback)
	}
}
