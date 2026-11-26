package client_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/client"
	"github.com/dualface/kander/server"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestNewUsesInjectedDefaultTransport(t *testing.T) {
	var paths []string
	client.SetDefaultTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "kander.invalid" {
			t.Errorf("host = %q", r.URL.Host)
		}
		paths = append(paths, r.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"server_version":"injected"}`))}, nil
	}))
	t.Cleanup(func() { client.SetDefaultTransport(nil) })
	for _, id := range []string{"client-a", "client-b"} {
		c, err := client.New("https://kander.invalid", id)
		if err != nil {
			t.Fatal(err)
		}
		version, err := c.Version(t.Context())
		if err != nil || version.ServerVersion != "injected" {
			t.Fatalf("version = %+v, %v", version, err)
		}
	}
	if want := "/api/v1/version,/api/v1/version"; strings.Join(paths, ",") != want {
		t.Fatalf("paths = %q, want %s", paths, want)
	}
}

func TestProjectDocumentReadsEmbeddedCardDocuments(t *testing.T) {
	ctx := t.Context()
	s, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	h := s.Handler()
	client.SetDefaultTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result(), nil
	}))
	t.Cleanup(func() { client.SetDefaultTransport(nil) })
	c, err := client.New("https://kander.invalid", "document-reader")
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Register(ctx, "documents", "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	card, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: "document", Title: "Read documents", Large: true})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/projects/" + info.ID + "/tasks/" + card.TaskID + "/documents/"
	for i, doc := range []api.Document{
		{Path: "plan.md", Text: "# Plan\n\nSchedule the update.\n"},
		{Path: "notes/detail.md", Text: "# 附件\r\n\r\nKeep the final space. "},
	} {
		doc.ExpectedRevision = card.Revision
		doc.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(doc.Text)))
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequestWithContext(ctx, http.MethodPut, base+doc.Path, strings.NewReader(string(data)))
		r.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
		r.Header.Set(api.HeaderIdempotencyKey, "write-document-"+strconv.Itoa(i))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("write %q: HTTP %d, %s", doc.Path, w.Code, w.Body)
		}
		if err := json.NewDecoder(w.Body).Decode(&card); err != nil {
			t.Fatal(err)
		}
		got, err := p.Document(ctx, card.TaskID, doc.Path)
		if err != nil || got.Path != doc.Path || got.Text != doc.Text {
			t.Fatalf("Document(%q) = %+v, %v", doc.Path, got, err)
		}
	}
	_, err = p.Document(ctx, card.TaskID, "notes/missing.md")
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Code != api.CodeNotFound {
		t.Fatalf("missing document error = %v, want not_found", err)
	}
}
