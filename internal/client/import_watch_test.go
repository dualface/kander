package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

func setDuration(t *testing.T, value *time.Duration, to time.Duration) {
	t.Helper()
	previous := *value
	*value = to
	t.Cleanup(func() { *value = previous })
}

// pacedStream answers with a progress line every interval for lines lines,
// then the result. Before the stream it reads the whole request body.
func pacedStream(t *testing.T, lines int, interval time.Duration) *httptest.Server {
	t.Helper()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", api.ImportStreamType)
		w.WriteHeader(http.StatusOK)
		for range lines {
			_, _ = w.Write([]byte(`{"progress":{"phase":"copy","done":1,"total":9}}` + "\n"))
			w.(http.Flusher).Flush()
			time.Sleep(interval)
		}
		_, _ = w.Write([]byte(`{"result":{"project":{"id":"p-1"},"files":3,"tasks":2,"snapshot":"s"}}` + "\n"))
	}))
	t.Cleanup(h.Close)
	return h
}

func wantIdleLoss(t *testing.T, err error) {
	t.Helper()
	var transport *TransportError
	if !errors.As(err, &transport) || !errors.Is(err, errImportIdle) || !strings.Contains(err.Error(), "run the same command again") {
		t.Fatalf("want a lost connection, got %#v", err)
	}
}

func loopbackImport(t *testing.T, url string, progress func(api.ImportProgress)) (api.ImportResult, error) {
	t.Helper()
	c, err := New(url, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	return c.ImportWithProgress(t.Context(), strings.Repeat("b", 64), api.ImportRequest{Project: "demo", From: "/kanban"}, progress)
}

func TestStreamedImportOutlastsTheTotalLimitWhileLinesArrive(t *testing.T) {
	setDuration(t, &importTimeout, 100*time.Millisecond)
	setDuration(t, &importStreamIdle, time.Second)
	h := pacedStream(t, 12, 30*time.Millisecond)
	if result, err := loopbackImport(t, h.URL, func(api.ImportProgress) {}); err != nil || result.Tasks != 2 {
		t.Fatalf("import: %+v %v", result, err)
	}
}

func TestStreamedImportEndsAsALostConnectionWhenLinesStop(t *testing.T) {
	setDuration(t, &importStreamIdle, 100*time.Millisecond)
	h := pacedStream(t, 2, time.Second)
	_, err := loopbackImport(t, h.URL, func(api.ImportProgress) {})
	wantIdleLoss(t, err)
}

func TestImportWithoutAStreamKeepsTheTotalLimit(t *testing.T) {
	setDuration(t, &importTimeout, 100*time.Millisecond)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(time.Second)
		_, _ = w.Write([]byte(`{"project":{"id":"p-1"},"files":3,"tasks":2,"snapshot":"s"}`))
	}))
	defer h.Close()
	for name, progress := range map[string]func(api.ImportProgress){"no stream asked": nil, "single JSON answer": func(api.ImportProgress) {}} {
		t.Run(name, func(t *testing.T) {
			started := time.Now()
			_, err := loopbackImport(t, h.URL, progress)
			var transport *TransportError
			if !errors.As(err, &transport) || time.Since(started) > 900*time.Millisecond {
				t.Fatalf("want the total limit, got %#v after %s", err, time.Since(started))
			}
		})
	}
}

func uploadTo(t *testing.T, url string, declared <-chan int64) (api.ImportResult, error) {
	t.Helper()
	c, err := New(url, "uploader")
	if err != nil {
		t.Fatal(err)
	}
	return c.UploadImport(t.Context(), "demo", io.NopCloser(strings.NewReader("archive")), declared, func(api.ImportProgress) {})
}

func TestUploadStreamUsesTheIdleLimit(t *testing.T) {
	setDuration(t, &importStreamIdle, 150*time.Millisecond)
	if result, err := uploadTo(t, pacedStream(t, 12, 30*time.Millisecond).URL, nil); err != nil || result.Tasks != 2 {
		t.Fatalf("paced upload: %+v %v", result, err)
	}
	_, err := uploadTo(t, pacedStream(t, 2, time.Second).URL, nil)
	wantIdleLoss(t, err)
}

func TestUploadRequestGetsTheReceiveDeadlineOfItsDeclaredBytes(t *testing.T) {
	setDuration(t, &importUploadSlack, 0)
	previous := importUploadDeadline
	t.Cleanup(func() { importUploadDeadline = previous })
	asked := make(chan int64, 2)
	importUploadDeadline = func(bytes int64) time.Duration {
		asked <- bytes
		return 50*time.Millisecond + time.Duration(bytes)*time.Millisecond
	}
	slow := func(delay time.Duration) string {
		h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			time.Sleep(delay)
			w.Header().Set("Content-Type", api.ImportStreamType)
			_, _ = w.Write([]byte(`{"result":{"project":{"id":"p-1"},"tasks":2}}` + "\n"))
		}))
		t.Cleanup(h.Close)
		return h.URL
	}
	declared := make(chan int64, 1)
	declared <- 400 // A deadline of 450ms from the start of the request.
	if result, err := uploadTo(t, slow(200*time.Millisecond), declared); err != nil || result.Tasks != 2 {
		t.Fatalf("an answer within the deadline of the declared bytes: %+v %v", result, err)
	}
	if first, second := <-asked, <-asked; first != 0 || second != 400 {
		t.Fatalf("deadline asked for %d then %d bytes", first, second)
	}
	declared <- 0
	if _, err := uploadTo(t, slow(400*time.Millisecond), declared); err == nil {
		t.Fatal("an answer after the deadline of the declared bytes was accepted")
	}
}
