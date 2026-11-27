package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

func uploadImport(ctx context.Context, target string, req api.ImportRequest, run importRun) (api.ImportResult, error) {
	var out api.ImportResult
	peer, err := client.New(target, "server-control")
	if err != nil {
		return out, err
	}
	if !api.ValidProjectName(req.Project) {
		return out, apiError(api.CodeInvalidRequest, "invalid project name: "+api.ProjectNameRule)
	}
	body, writer := io.Pipe()
	produceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	produced := make(chan error, 1)
	declared := make(chan int64, 1)
	go func() {
		err := writeBoardUpload(produceCtx, writer, req, declared, run.report)
		if err == nil && run.uploadSent != nil {
			run.uploadSent.Store(true)
		}
		_ = writer.CloseWithError(err)
		produced <- err
	}()
	out, err = peer.UploadImport(ctx, req.Project, body, declared, run.report)
	cancel()
	_ = body.Close()
	writeErr := <-produced
	if errors.Is(err, client.ErrImportUnsupported) {
		return out, errors.New(config.Text("server.import_upload_unsupported", target))
	}
	if writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) && !errors.Is(writeErr, context.Canceled) {
		// A body producer error can be nested in a transport failure, or
		// the server can see only a truncated request.
		return out, writeErr
	}
	return out, uploadLimitHint(err)
}

// uploadLimitHint adds to a 413 refusal that an older server has lower
// limits: this client no longer refuses a large board itself.
func uploadLimitHint(err error) error {
	var refused *api.Error
	if !errors.As(err, &refused) || refused.Status != http.StatusRequestEntityTooLarge {
		return err
	}
	hinted := *refused
	hinted.Message += "; " + config.Text("server.import_upload_limit_hint")
	return &hinted
}

// writeBoardUpload sends declared the file bytes of the manifest before the
// first file body.
func writeBoardUpload(ctx context.Context, dst io.Writer, req api.ImportRequest, declared chan<- int64, report func(api.ImportProgress)) error {
	progress := newImportEmitter(report)
	defer progress.stop()
	progress.phase(api.ImportPhaseLock)
	if err := board.RecoverTransactionsContext(ctx, req.From); err != nil {
		return fmt.Errorf("recover the source board first: %w", err)
	}
	return board.WithImportSnapshot(ctx, req.From, func() error {
		// Only the format is checked here. The server applies its own
		// limits to the manifest, before any file body is sent.
		limits := clientUploadLimits()
		progress.phase(api.ImportPhaseScan)
		tree, err := scanImportTree(ctx, req.From, progress.counter(api.ImportPhaseScan, 0), true)
		if err != nil {
			return err
		}
		if err := checkImportRecordLayout(tree.files); err != nil {
			return err
		}
		m := api.ImportUploadManifest{ImportRequest: req, Snapshot: tree.digest, Directories: tree.dirs}
		m.Files = make([]api.ImportUploadFile, 0, len(tree.files))
		for _, rel := range tree.files {
			sum := tree.sums[rel]
			m.Files = append(m.Files, api.ImportUploadFile{Path: rel, Size: tree.sizes[rel], SHA256: hex.EncodeToString(sum[:])})
		}
		if declared != nil {
			declared <- tree.bytes
		}
		if err := writeUploadManifest(dst, m, limits); err != nil {
			return err
		}
		progress.phaseCounts(api.ImportPhaseUpload, 0, len(tree.files), 0, tree.bytes)
		w := &uploadProgressWriter{ctx: ctx, dst: dst, total: tree.bytes, files: len(tree.files), progress: progress}
		for _, file := range m.Files {
			hash := sha256.New()
			n, err := fs.CopyRegularFile(io.MultiWriter(w, hash), req.From, filepath.Join(req.From, filepath.FromSlash(file.Path)), file.Size)
			if err != nil {
				return err
			}
			if n != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
				return sourceChangedDuringUpload()
			}
			w.done++
		}
		again, err := scanImportTree(ctx, req.From, nil, true)
		if err != nil {
			return err
		}
		if again.digest != tree.digest || !slices.Equal(again.dirs, tree.dirs) {
			return sourceChangedDuringUpload()
		}
		// Keep the same phase throughout the final source scan. The server
		// cannot publish until this marker and the successful EOF arrive.
		_, err = io.WriteString(dst, api.ImportUploadComplete)
		return err
	})
}

func sourceChangedDuringUpload() error {
	return importRejected("source board changed during upload; nothing was published")
}

func writeUploadManifest(dst io.Writer, m api.ImportUploadManifest, limits uploadLimits) error {
	if err := checkUploadManifest(m, limits); err != nil {
		return err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(payload) > limits.manifest {
		return uploadTooLarge("manifest bytes", int64(len(payload)), int64(limits.manifest))
	}
	if err := binary.Write(dst, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err = dst.Write(payload)
	return err
}

type uploadProgressWriter struct {
	ctx         context.Context
	dst         io.Writer
	progress    *importEmitter
	total, sent int64
	done, files int
	last        time.Time
}

func (w *uploadProgressWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.dst.Write(data)
	w.sent += int64(n)
	if time.Since(w.last) >= time.Second || w.sent == w.total {
		w.last = time.Now()
		w.progress.count(api.ImportPhaseUpload, w.done, w.files, w.sent, w.total)
	}
	return n, err
}
