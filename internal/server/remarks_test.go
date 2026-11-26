package server

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
)

func TestOnlineImportRoutesSourceRemarks(t *testing.T) {
	source := v1Board(t)
	var fallback []string
	old := board.Remark
	board.Remark = func(code, _ string) { fallback = append(fallback, code) }
	t.Cleanup(func() { board.Remark = old })

	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	s, err := Open(Options{DataDir: t.TempDir(), Logger: logger, Remarks: func(code, text string) {
		logger.Info("board remark", "code", code, "text", strings.TrimRight(text, "\r\n"))
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	// Register first so the project-open remark cannot satisfy the source assertion.
	if _, _, err := s.register(api.RegisterProject{Name: "online"}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	h := httptest.NewServer(s.Handler())
	t.Cleanup(h.Close)
	if _, err := onlineImport(t, s, h.URL, api.ImportRequest{Project: "online", From: source}); err != nil {
		t.Fatal(err)
	}
	h.Close()
	const partitioned = "board_journal_partitioned"
	if got := output.String(); strings.Count(got, `msg="board remark" code=`+partitioned) != 1 || len(fallback) != 0 {
		t.Fatalf("source remark not routed to its instance: fallback %v, log:\n%s", fallback, got)
	}
	if err := board.RecoverTransactions(source); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fallback, []string{partitioned}) {
		t.Fatalf("source remark sink outlived import: fallback %v", fallback)
	}
}

func TestStandaloneProjectOpenAndOfflineImportUseBoardRemark(t *testing.T) {
	source := v1Board(t)
	var fallback []string
	old := board.Remark
	board.Remark = func(code, _ string) { fallback = append(fallback, code) }
	t.Cleanup(func() { board.Remark = old })
	options := Options{DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler)}
	s, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.register(api.RegisterProject{Name: "offline"}); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	const partitioned = "board_journal_partitioned"
	if !slices.Equal(fallback, []string{partitioned}) {
		t.Fatalf("standalone registration bypassed board.Remark: %v", fallback)
	}
	fallback = nil
	s, err = Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if !slices.Equal(fallback, []string{partitioned}) {
		t.Fatalf("standalone project recovery bypassed board.Remark: %v", fallback)
	}
	fallback = nil
	if _, err := s.ImportWithProgress(t.Context(), "offline", "", source, func(api.ImportProgress) {}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fallback, []string{partitioned}) {
		t.Fatalf("offline import bypassed board.Remark: %v", fallback)
	}
}
