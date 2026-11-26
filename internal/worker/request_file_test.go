package worker

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/process"
)

func TestMaximumRequestInPrivateFileDoesNotEnlargePrompt(t *testing.T) {
	task := strings.Repeat("t", 80)
	message := "中\n" + strings.Repeat("界", (api.MaxRequestMessage-4)/3)
	request := api.CollabRequest{ID: "req-" + strings.Repeat("a", 16), Kind: api.RequestNote, Message: message, TargetGeneration: 3}
	if len(message) != api.MaxRequestMessage {
		t.Fatalf("payload fixture has %d bytes", len(message))
	}
	marker, otherMarker := newAckMarker(), newAckMarker()
	if !regexp.MustCompile(`^KANDER-NOTIFY-ACK:[0-9a-f]{32}$`).MatchString(marker) || marker == otherMarker {
		t.Fatal("each delivery needs a fresh 128-bit marker")
	}
	body := requestBody(task, request, marker)
	dir, path, err := testRequestFiles(t)(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	bodyStart := strings.Index(text, "generation 3\n\n")
	if bodyStart == -1 {
		t.Fatal("request file lacks the generation")
	}
	bodyStart += len("generation 3\n\n")
	if text[bodyStart:bodyStart+len(message)] != message {
		t.Fatal("full request body did not survive byte for byte")
	}
	show := "kander request show " + task + " " + request.ID + " --json"
	answer := "kander request answer " + task + " " + request.ID + " done|rejected [note]"
	if !strings.HasPrefix(text, ackInstruction+"\n"+marker+"\n") || !strings.Contains(text, show) || !strings.Contains(text, answer) {
		t.Fatal("file lacks the acknowledgement, show or answer instructions")
	}
	shortPath := filepath.Join(string(os.PathSeparator), "tmp", "kander-request-"+strings.Repeat("a", 32), "request.md")
	line := requestPointer(task, request, shortPath)
	if len(line) > 512 || strings.ContainsAny(line, "中界\n\r") || strings.Contains(line, marker) {
		t.Fatalf("private message or marker in prompt, or prompt exceeds 512 bytes: %q (%d)", line, len(line))
	}
	if !strings.Contains(line, process.AgentPath(shortPath)) || !strings.HasSuffix(line, show) {
		t.Fatalf("short instruction lacks rendered path or fallback: %q", line)
	}
	request.Message = "another message"
	if same := requestPointer(task, request, shortPath); same != line {
		t.Fatal("prompt must not depend on the message")
	}
}

func pathFromRequestPointer(t *testing.T, line string) string {
	t.Helper()
	_, rest, ok := strings.Cut(line, ": read ")
	if !ok {
		t.Fatalf("request pointer lacks the path: %q", line)
	}
	quoted, _, _ := strings.Cut(rest, " and follow it;")
	path, err := strconv.Unquote(quoted)
	if err != nil {
		t.Fatalf("request path is not quoted: %v", err)
	}
	return filepath.FromSlash(path)
}

func TestRequestFileIsClosedOnNormalWorkerExit(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("close-on-exit", true)
	h.request(c, "normal shutdown must close the retained file")
	o := h.workerOptions()
	o.Tick = 10 * time.Millisecond
	h.advance(16 * time.Second)
	stop := sync.OnceFunc(serveWorker(t, o))
	defer stop()
	waitFor(t, "one typed request", func() bool { return len(h.deliveredLines()) == 1 })
	dir := filepath.Dir(pathFromRequestPointer(t, h.deliveredLines()[0]))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("file is not present while worker is running: %v", err)
	}
	stop()
	requireRequestDirRemoved(t, dir)
}

func TestRequestFileCreationFailureKeepsReservationRecoverable(t *testing.T) {
	h := newHarness(t)
	_, c := h.claimed("recoverable-file", true)
	r := h.request(c, "the request body must survive recovery")
	create := h.loop.o.CreateRequestFile
	h.loop.o.CreateRequestFile = func(string) (*fs.TempDir, string, error) {
		return nil, "", errors.New("simulated private file error")
	}
	h.workAfter(16 * time.Second)
	if got := h.requests(c)[0]; got.Note != reservationNote || len(h.lines) != 0 || slices.Contains(h.credential().Delivered, r.ID) {
		t.Fatalf("failed file write started typing or changed reservation: %+v", got)
	}
	h.loop.o.CreateRequestFile = create
	h.workAfter(16 * time.Second)
	if got := h.requests(c)[0]; len(h.lines) != 1 || got.Note != typedNote {
		t.Fatalf("reservation was not recovered via the private file: %+v", got)
	}
	data, err := os.ReadFile(pathFromRequestPointer(t, h.lines[0]))
	if err != nil || !strings.Contains(string(data), r.Message) || strings.Contains(h.lines[0], r.Message) {
		t.Fatalf("the recovered request did not keep its body in the file: %v", err)
	}
}
