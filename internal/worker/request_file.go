package worker

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/process"
)

const (
	requestFileRetention = 30 * time.Minute
	ackMarkerPrefix      = "KANDER-NOTIFY-ACK:"
	ackInstruction       = "Acknowledge this notification: before anything else, output this line exactly:"
)

func newAckMarker() string {
	var random [16]byte
	rand.Read(random[:])
	return fmt.Sprintf("%s%x", ackMarkerPrefix, random)
}

// requestPointer is the only line typed. Neither the body nor the marker may
// reach the composer, so a draft alone cannot acknowledge the request.
func requestPointer(task string, r api.CollabRequest, path string) string {
	task, id, kind := stripControl(task), stripControl(r.ID), stripControl(r.Kind)
	return fmt.Sprintf("# kander-request %s %s for %s: read %q and follow it; output its acknowledgement line first; if missing, run kander request show %s %s --json",
		id, kind, task, process.AgentPath(path), task, id)
}

func requestBody(task string, r api.CollabRequest, marker string) string {
	var extra string
	if r.Kind == api.RequestNote {
		extra = "no dispatch is attached: handle the message within the card contract; do not run kander dispatch prepare for it and do not use it as the grant of a dispatch-bound move\n"
	} else if r.Dispatch != nil {
		extra = fmt.Sprintf("this is a proposal of dispatch %s: do not run a bare kander move; first write the dispatch object of the request unchanged to an intent file, run kander dispatch prepare on it, then run kander move with --dispatch-id and --execution-epoch from its output\n", stripControl(r.Dispatch.ID))
	}
	return fmt.Sprintf("%s\n%s\n\nkander-request %s %s for %s from %s, generation %d\n\n%s\n\n%sinspect the full request and its proposed material: kander request show %s %s --json; do not act on superseded requests or another execution generation\nanswer: kander request answer %s %s done|rejected [note]\n",
		ackInstruction, marker, r.ID, r.Kind, task, r.FromClient, r.TargetGeneration, r.Message, extra, task, r.ID, task, r.ID)
}

type requestFile struct {
	dir     *fs.TempDir
	expires time.Time
	cancel  context.CancelFunc
}

// deliveryScope keeps request files and observers together under a credential.
// Only the work loop owns files. Observers only publish to results under mu.
type deliveryScope struct {
	ctx     context.Context
	cancel  context.CancelFunc
	files   map[string]requestFile
	mu      sync.Mutex
	results map[string]ackResult
}

func (w *loop) keepRequestFile(ctx context.Context, key credentialKey, id string, dir *fs.TempDir, expires time.Time) *deliveryScope {
	scope := w.deliveries[key]
	if scope == nil {
		ctx, cancel := context.WithCancel(ctx)
		scope = &deliveryScope{ctx: ctx, cancel: cancel, files: map[string]requestFile{}, results: map[string]ackResult{}}
		w.deliveries[key] = scope
	}
	scope.files[id] = requestFile{dir: dir, expires: expires}
	return scope
}

func (w *loop) closeRequestDir(log *slog.Logger, id string, dir *fs.TempDir) {
	if err := dir.Close(); err != nil {
		log.Warn("removing the private request file failed", "request", id, "error", err)
	}
}

func (w *loop) closeRequestFile(scope *deliveryScope, id string) {
	file, ok := scope.files[id]
	if !ok {
		return
	}
	if file.cancel != nil {
		file.cancel()
	}
	scope.dropResult(id)
	delete(scope.files, id)
	w.closeRequestDir(w.o.Logger, id, file.dir)
}

func (w *loop) closeDeliveryScope(key credentialKey) {
	if scope := w.deliveries[key]; scope != nil {
		scope.cancel()
		for id := range scope.files {
			w.closeRequestFile(scope, id)
		}
		delete(w.deliveries, key)
	}
}

func (w *loop) closeRequestFiles() {
	for key := range w.deliveries {
		w.closeDeliveryScope(key)
	}
}

func (w *loop) expireRequestFiles(now time.Time) {
	for _, scope := range w.deliveries {
		for id, file := range scope.files {
			if !now.Before(file.expires) {
				w.closeRequestFile(scope, id)
			}
		}
	}
}

func (w *loop) closeSettledRequestFiles(key credentialKey, requests []api.CollabRequest) {
	scope := w.deliveries[key]
	if scope == nil {
		return
	}
	for _, r := range requests {
		switch r.Status {
		case api.RequestDone, api.RequestRejected, api.RequestSuperseded:
			w.closeRequestFile(scope, r.ID)
		}
	}
}
