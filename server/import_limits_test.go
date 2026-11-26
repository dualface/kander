package server_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/server"
)

// uploadArchive packs a board of the seven states and the given files.
func uploadArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	m := api.ImportUploadManifest{ImportRequest: api.ImportRequest{Project: "demo", From: "/source/kanban"},
		Directories: []string{"archived", "backlog", "done", "review", "todo", "trash", "working"}}
	var body string
	digest := sha256.New()
	for _, path := range []string{"backlog/a.md", "backlog/b.md"} {
		contents, ok := files[path]
		if !ok {
			continue
		}
		sum := sha256.Sum256([]byte(contents))
		m.Files = append(m.Files, api.ImportUploadFile{Path: path, Size: int64(len(contents)), SHA256: hex.EncodeToString(sum[:])})
		fmt.Fprintf(digest, "%s\x00%x\n", path, sum)
		body += contents
	}
	m.Snapshot = hex.EncodeToString(digest.Sum(nil))
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	wire := binary.BigEndian.AppendUint32(nil, uint32(len(manifest)))
	wire = append(wire, manifest...)
	return append(wire, body+api.ImportUploadComplete...)
}

func openUploads(t *testing.T, limits *api.ImportUploadLimits) *httptest.Server {
	t.Helper()
	s, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), ImportUploadLimits: limits})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s.Handler())
	t.Cleanup(func() { h.Close(); s.Close() })
	return h
}

// postUpload sends the head of an upload request, declaring length body
// bytes, then body, and returns the status and the error message.
func postUpload(t *testing.T, h *httptest.Server, length int64, body []byte) (int, string) {
	t.Helper()
	conn, err := net.Dial("tcp", h.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /api/v1/projects/demo/import HTTP/1.1\r\nHost: test\r\nContent-Type: %s\r\n%s: %d\r\nContent-Length: %d\r\n\r\n",
		api.ImportUploadType, api.HeaderProtocol, api.ProtocolVersion, length)
	if _, err := conn.Write(body); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var e api.Error
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, e.Message
}

func TestEmbedderUploadLimitsBoundTheRequestAndTheManifest(t *testing.T) {
	limits := api.ImportUploadLimits{Files: 1, Dirs: 100, Bytes: 1 << 10, FileBytes: 1 << 10, Manifest: 1 << 12}
	h := openUploads(t, &limits)
	wire := limits.Bytes + int64(limits.Manifest) + 4 + int64(len(api.ImportUploadComplete))
	status, message := postUpload(t, h, wire+1, nil)
	if status != http.StatusRequestEntityTooLarge || message != fmt.Sprintf("board upload exceeds the request bytes limit: declared %d, limit %d", wire+1, wire) {
		t.Fatalf("request limit: %d %q", status, message)
	}
	archive := uploadArchive(t, map[string]string{"backlog/a.md": "a", "backlog/b.md": "b"})
	status, message = postUpload(t, h, int64(len(archive)), archive)
	if status != http.StatusRequestEntityTooLarge || message != "board upload exceeds the files limit: declared 2, limit 1" {
		t.Fatalf("manifest limit: %d %q", status, message)
	}
}

func TestEmbedderWithoutUploadLimitsUsesTheDefaults(t *testing.T) {
	h := openUploads(t, nil)
	defaults := api.DefaultImportUploadLimits()
	wire := defaults.Bytes + int64(defaults.Manifest) + 4 + int64(len(api.ImportUploadComplete))
	status, message := postUpload(t, h, wire+1, nil)
	if status != http.StatusRequestEntityTooLarge || !strings.HasSuffix(message, "limit "+strconv.FormatInt(wire, 10)) {
		t.Fatalf("default request limit: %d %q", status, message)
	}
}

func TestEmbedderUploadLimitsMustBeUsable(t *testing.T) {
	valid := api.DefaultImportUploadLimits()
	for name, change := range map[string]func(*api.ImportUploadLimits){
		"files":         func(l *api.ImportUploadLimits) { l.Files = 0 },
		"directories":   func(l *api.ImportUploadLimits) { l.Dirs = -1 },
		"bytes":         func(l *api.ImportUploadLimits) { l.Bytes = 0 },
		"file bytes":    func(l *api.ImportUploadLimits) { l.FileBytes = 0 },
		"manifest":      func(l *api.ImportUploadLimits) { l.Manifest = 0 },
		"manifest bits": func(l *api.ImportUploadLimits) { l.Manifest = math.MaxUint32 + 1 },
		"request size":  func(l *api.ImportUploadLimits) { l.Bytes = math.MaxInt64 },
	} {
		t.Run(name, func(t *testing.T) {
			limits := valid
			change(&limits)
			if s, err := server.Open(server.Options{DataDir: t.TempDir(), ImportUploadLimits: &limits}); err == nil {
				s.Close()
				t.Fatalf("Open accepted %+v", limits)
			}
		})
	}
}
