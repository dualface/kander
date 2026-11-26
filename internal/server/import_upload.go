package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
)

const (
	maxImportUploads = 2
	uploadPrefix     = "upload."
)

func (s *Server) importUploadRoute(r *http.Request) outcome {
	name := r.PathValue("name")
	if !api.ValidProjectName(name) {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid project name: "+api.ProjectNameRule)}
	}
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != api.ImportUploadType {
		return outcome{err: apiError(api.CodeInvalidRequest, "unsupported board upload format")}
	}
	return outcome{stream: func(w http.ResponseWriter) int {
		return s.handleImportUpload(w, r, name, s.upload)
	}}
}

func (s *Server) handleImportUpload(w http.ResponseWriter, r *http.Request, name string, limits uploadLimits) int {
	received := false
	reject := func(err error) int {
		if !received && r.ProtoMajor == 1 {
			// Do not drain untrusted bytes before returning an early refusal.
			w.Header().Set("Connection", "close")
		}
		var refused *uploadReceiveError
		if errors.As(err, &refused) {
			s.log.Warn("board upload refused", "project_name", name, "stage", refused.stage, "error", refused.cause)
		}
		e := s.failure(r, err)
		writeJSONResponse(w, e.Status, e)
		return e.Status
	}
	if r.ContentLength > limits.wireBytes() {
		return reject(uploadTooLarge("request bytes", r.ContentLength, limits.wireBytes()))
	}
	s.mu.Lock()
	if s.closing.Load() {
		s.mu.Unlock()
		return reject(closingError())
	}
	select {
	case s.uploadSlots <- struct{}{}:
		s.uploadWorkers.Add(1)
		s.mu.Unlock()
	default:
		s.mu.Unlock()
		return reject(apiError(api.CodeNotReady, "board upload capacity reached; retry later"))
	}
	defer func() {
		<-s.uploadSlots
		s.uploadWorkers.Done()
	}()

	// The receive deadline starts with the fixed part, which covers the
	// manifest, and grows with the bytes the manifest declares.
	started := time.Now()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	rc := http.NewResponseController(w)
	receive, err := newUploadReceive(rc, cancel, started.Add(limits.timeout))
	if err != nil {
		return reject(errors.New("cannot enforce board upload request deadline"))
	}
	defer receive.finish()
	shutdownApplied := make(chan struct{})
	stopShutdown := context.AfterFunc(s.shutdown, func() {
		defer close(shutdownApplied)
		receive.stop()
	})
	defer func() {
		if !stopShutdown() {
			<-shutdownApplied
		}
	}()

	m, err := readUploadManifest(r.Body, limits)
	if err != nil {
		return reject(err)
	}
	receive.extend(started.Add(limits.deadline(declaredUploadBytes(m))))
	if m.Project != name {
		return reject(invalidUpload())
	}
	for _, state := range board.States {
		if _, ok := slices.BinarySearch(m.Directories, state); !ok {
			return reject(importRejected("source board is missing the directory " + state))
		}
	}
	projectRel := filepath.Join("projects", api.ProjectDirName(name))
	if err := ensureDir(s.data, projectRel); err != nil {
		s.log.Error("create board upload parent", "error", err)
		return reject(errors.New("cannot create board upload staging"))
	}
	stage, err := fs.CreatePrivateTempDir(filepath.Join(s.data, projectRel), uploadPrefix)
	if err != nil {
		s.log.Error("create board upload staging", "error", err)
		return reject(errors.New("cannot create board upload staging"))
	}
	cleanStage := sync.OnceFunc(func() {
		if err := s.removeUploadStage(stage); err != nil {
			s.log.Error("remove board upload staging", "project_name", name, "error", err)
		}
	})
	defer cleanStage()
	if err := receiveUpload(ctx, r.Body, stage.Path, m, receive, limits.idle); err != nil {
		return reject(err)
	}
	// The request is completely consumed before starting a response. This
	// works with HTTP/1 without full duplex and treats truncation as refusal.
	received = true
	receive.finish()
	if err := checkImportRecordLayout(uploadPaths(m)); err != nil {
		return reject(err)
	}
	if err := board.CheckRecoveredImportJournal(stage.Path); err != nil {
		return reject(importRejected("uploaded board has invalid or unrecovered transaction history"))
	}
	// The archive is complete and owned by the server. Its import follows
	// the rules of a local import: it is not bound by the receive deadline
	// and stops only when the client goes away or the server shuts down.
	importCtx, finish := context.WithCancel(r.Context())
	stopImportShutdown := context.AfterFunc(s.shutdown, finish)
	defer func() {
		stopImportShutdown()
		finish()
	}()
	run := func(report func(api.ImportProgress)) (api.ImportResult, error) {
		progress := newImportEmitter(report)
		defer progress.stop()
		progress.phase(api.ImportPhaseLock)
		value, err := s.importPreparedSource(importCtx, m.ImportRequest, stage.Path, m.From, m.Snapshot, progress)
		if err != nil {
			// Log the original error first: the client conversion below
			// drops the text of an unclassified failure.
			s.logImportFailure(name, err)
		}
		cleanStage()
		value.Project.BoardPath = ""
		if err != nil {
			err = safeUploadImportError(err, s.data, stage.Path, m.From)
		}
		return value, err
	}
	if strings.Contains(r.Header.Get("Accept"), api.ImportStreamType) {
		return s.streamImport(w, r, run)
	}
	value, err := run(nil)
	if err != nil {
		return reject(err)
	}
	writeJSONResponse(w, http.StatusOK, value)
	return http.StatusOK
}

// removeUploadStaging deletes an upload staging directory whatever the
// number of its entries. The private temporary directory cleanup has an entry
// budget that a board upload exceeds, so the server-created contents are
// removed first with os.RemoveAll, which does not follow links, and the
// TempDir then removes its empty root.
func removeUploadStaging(stage *fs.TempDir) error {
	entries, err := os.ReadDir(stage.Path)
	for _, entry := range entries {
		err = errors.Join(err, os.RemoveAll(filepath.Join(stage.Path, entry.Name())))
	}
	return errors.Join(err, stage.Close())
}

func uploadPaths(m api.ImportUploadManifest) []string {
	paths := make([]string, len(m.Files))
	for i, file := range m.Files {
		paths[i] = file.Path
	}
	return paths
}

// receiveUpload writes the file bodies into staging. While they arrive, a
// stall longer than idle ends the request; after the last one, the client
// scans its source again before the marker, so only the absolute deadline
// applies to the marker and the end of the request.
func receiveUpload(ctx context.Context, body io.Reader, staging string, m api.ImportUploadManifest, receive *uploadReceive, idle time.Duration) error {
	for _, rel := range m.Directories {
		if err := ctx.Err(); err != nil {
			return uploadReceiveFailed("directories", err)
		}
		if err := fs.CreatePrivateDirectory(staging, filepath.Join(staging, filepath.FromSlash(rel))); err != nil {
			if errors.Is(err, os.ErrExist) {
				return invalidUpload()
			}
			return err
		}
	}
	buf := make([]byte, 64<<10)
	receive.watchIdle(idle)
	files := uploadBodyReader{r: body, receive: receive}
	for _, file := range m.Files {
		if err := ctx.Err(); err != nil {
			return uploadReceiveFailed("file bodies", err)
		}
		if err := receiveUploadFile(staging, files, file, buf); err != nil {
			return err
		}
	}
	receive.watchIdle(0)
	// Demand a successful request EOF, not merely the declared file bytes.
	// The sender emits this marker only after its final source snapshot.
	var marker [len(api.ImportUploadComplete)]byte
	if _, err := io.ReadFull(body, marker[:]); err != nil {
		return uploadReceiveFailed("completion marker", err)
	}
	if string(marker[:]) != api.ImportUploadComplete {
		return uploadReceiveFailed("completion marker", errors.New("wrong completion marker"))
	}
	var extra [1]byte
	switch _, err := io.ReadFull(body, extra[:]); {
	case err == nil:
		return uploadReceiveFailed("end of request", errors.New("bytes follow the completion marker"))
	case err != io.EOF:
		return uploadReceiveFailed("end of request", err)
	case ctx.Err() != nil:
		return uploadReceiveFailed("end of request", ctx.Err())
	}
	return nil
}

func receiveUploadFile(root string, body io.Reader, file api.ImportUploadFile, buf []byte) error {
	f, err := fs.CreatePrivateRegularFile(root, filepath.Join(root, filepath.FromSlash(file.Path)))
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return invalidUpload()
		}
		return err
	}
	hash := sha256.New()
	n, copyErr := io.CopyBuffer(io.MultiWriter(f, hash), io.LimitReader(body, file.Size), buf)
	closeErr := f.Close()
	var diskError *os.PathError
	if errors.As(copyErr, &diskError) || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	switch {
	case copyErr != nil:
		return uploadReceiveFailed("file body "+file.Path, copyErr)
	case n != file.Size:
		return uploadReceiveFailed("file body "+file.Path, io.ErrUnexpectedEOF)
	case hex.EncodeToString(hash.Sum(nil)) != file.SHA256:
		return uploadReceiveFailed("file body "+file.Path, errors.New("digest differs from the manifest"))
	}
	return nil
}

// uploadReceiveError is a refused upload body. The client receives the
// generic board_rejected answer; the server log keeps the stage and cause.
type uploadReceiveError struct {
	stage string
	cause error
}

func uploadReceiveFailed(stage string, cause error) error {
	return &uploadReceiveError{stage: stage, cause: cause}
}

func (e *uploadReceiveError) Error() string {
	return "board upload refused at " + e.stage + ": " + e.cause.Error()
}

func (e *uploadReceiveError) Unwrap() error { return e.cause }

func (e *uploadReceiveError) As(target any) bool {
	if out, ok := target.(**api.Error); ok {
		*out = invalidUpload().(*api.Error)
		return true
	}
	return false
}

func safeUploadImportError(err error, data, staging, source string) error {
	e := *asAPIError(err)
	// A target or registry diagnostic may contain a server-side path.
	// Replace both raw and quoted spellings before returning anything.
	e.Message = strings.NewReplacer(
		quotedPath(staging), quotedPath(source), staging, source,
		quotedPath(filepath.Dir(staging)), "<destination project>", filepath.Dir(staging), "<destination project>",
		quotedPath(data), "<server data>", data, "<server data>",
	).Replace(e.Message)
	return &e
}

// cleanupUploads runs under the exclusive data directory lock before ready.
// Include unregistered project roots left by a client that never reached import.
func (s *Server) cleanupUploads() error {
	projects := filepath.Join(s.data, "projects")
	exists, err := fs.DirectoryExists(s.data, projects)
	if err != nil || !exists {
		return err
	}
	entries, err := fs.ListDirectory(s.data, projects)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Kind != fs.KindDirectory {
			continue
		}
		project := filepath.Join(projects, entry.Name)
		children, err := fs.ListDirectory(s.data, project)
		if err != nil {
			return err
		}
		for _, child := range children {
			if strings.HasPrefix(child.Name, uploadPrefix) {
				if err := removeImportDirectory(project, filepath.Join(project, child.Name)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
