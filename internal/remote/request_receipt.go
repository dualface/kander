package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

type requestCall struct {
	Schema    int                  `json:"schema"`
	ServerURL string               `json:"server_url"`
	ProjectID string               `json:"project_id"`
	ClientID  string               `json:"client_id"`
	TaskID    string               `json:"task_id"`
	Key       string               `json:"key"`
	Input     api.NewCollabRequest `json:"input"`
	Phase     string               `json:"phase"`
	Result    *api.CollabRequest   `json:"result,omitempty"`
	UpdatedAt time.Time            `json:"updated_at"`
}

// RecoverRequest reconciles an earlier uncertain call before a command derives
// a new generation, baseline or dispatch ID. It never substitutes fresh inputs.
func (s *Session) RecoverRequest(ctx context.Context, logical any) (api.CollabRequest, bool, error) {
	return s.requestCall(ctx, logical, nil)
}

// SendRequest persists the operation key and immutable input before transport.
// A completed call permits a later intentional message; an uncertain one must
// reconcile its original key and subject first, including after process restart.
func (s *Session) SendRequest(ctx context.Context, key, task string, input api.NewCollabRequest, logical any) (api.CollabRequest, error) {
	input.FromClient = s.Client.ClientID()
	if key == "" {
		key = client.NewKey()
	}
	call := &requestCall{Schema: 1, ServerURL: s.ServerURL, ProjectID: s.Info.ID, ClientID: s.Client.ClientID(), TaskID: task, Key: key, Input: input, Phase: "pending"}
	value, _, err := s.requestCall(ctx, logical, call)
	return value, err
}

func (s *Session) requestCall(ctx context.Context, logical any, fresh *requestCall) (value api.CollabRequest, found bool, err error) {
	identity, err := json.Marshal(logical)
	if err != nil {
		return value, false, err
	}
	sum := sha256.Sum256(identity)
	root, err := s.LocalDataRoot()
	if err != nil {
		return value, false, err
	}
	dir := filepath.Join(root, "request-calls")
	if err := fs.EnsurePrivateDirectory(s.Home, dir, true); err != nil {
		return value, false, err
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
	file, err := fs.OpenLockFile(s.Home, path+".lock")
	if err != nil {
		return value, false, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		return value, false, err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	data, exists, err := fs.ReadRegularFileIfExists(s.Home, path)
	if err != nil {
		return value, false, err
	}
	var stored requestCall
	if exists {
		if err := json.Unmarshal(data, &stored); err != nil {
			return value, false, errors.New(config.Text("remote.request_receipt_damaged", path))
		}
		if stored.Schema != 1 || stored.Phase != "pending" && stored.Phase != "complete" && stored.Phase != "failed" || stored.ServerURL != s.ServerURL || stored.ProjectID != s.Info.ID || stored.ClientID != s.Client.ClientID() || !api.ValidIdentity(stored.TaskID) || stored.Key == "" {
			return value, false, errors.New(config.Text("remote.request_receipt_damaged", path))
		}
		if stored.Phase == "pending" {
			fresh = &stored
		}
	}
	if fresh == nil {
		return value, false, nil
	}
	found = true
	store := func() error {
		fresh.UpdatedAt = time.Now().UTC()
		data, err := json.Marshal(fresh)
		if err != nil {
			return err
		}
		return fs.WriteTextAtomic(s.Home, path, string(data), true)
	}
	if exists && stored.Phase == "pending" {
		known, present, readErr := s.inspectRequestCall(ctx, *fresh, "")
		if readErr != nil {
			return value, true, fmt.Errorf("%s: %w", config.Text("remote.request_pending", path, api.CollaborationRequestID(fresh.Key)), readErr)
		}
		if present {
			fresh.Phase, fresh.Result = "complete", &known
			return known, true, store()
		}
	}
	if err := store(); err != nil {
		return value, true, err
	}
	value, sendErr := s.Project.CreateRequest(ctx, fresh.Key, fresh.TaskID, fresh.Input)
	if sendErr == nil && !requestFactsMatch(*fresh, value) {
		return value, true, errors.New(config.Text("remote.request_recovery_mismatch"))
	}
	// Inspect central facts even after a response was lost. A typed dispatch may
	// also have a canonical request from an earlier identical operation key.
	if known, present, _ := s.inspectRequestCall(ctx, *fresh, value.ID); present {
		value, sendErr = known, nil
	}

	if sendErr != nil {
		if requestFailureKnown(sendErr) {
			fresh.Phase = "failed"
			if saveErr := store(); saveErr != nil {
				return value, true, errors.Join(sendErr, saveErr)
			}
		}
		if fresh.Phase == "pending" {
			return value, true, fmt.Errorf("%s: %w", config.Text("remote.request_pending", path, api.CollaborationRequestID(fresh.Key)), sendErr)
		}
		return value, true, sendErr
	}
	fresh.Phase, fresh.Result = "complete", &value
	return value, true, store()
}

func (s *Session) inspectRequestCall(ctx context.Context, call requestCall, preferred string) (api.CollabRequest, bool, error) {
	requests, err := s.Project.Requests(ctx, call.TaskID)
	if err != nil {
		return api.CollabRequest{}, false, err
	}
	for _, candidate := range requests {
		exact := candidate.ID == api.CollaborationRequestID(call.Key) || preferred != "" && candidate.ID == preferred
		idMatch := exact || call.Input.Dispatch != nil && candidate.Dispatch != nil && candidate.Dispatch.ID == call.Input.Dispatch.ID
		if !idMatch {
			continue
		}
		if requestFactsMatch(call, candidate) {
			return candidate, true, nil
		}
		if exact {
			return candidate, false, errors.New(config.Text("remote.request_recovery_mismatch"))
		}
	}
	return api.CollabRequest{}, false, nil
}

func requestFactsMatch(call requestCall, value api.CollabRequest) bool {
	in := call.Input
	if value.TaskID != call.TaskID || value.FromClient != call.ClientID || value.TargetGeneration != in.TargetGeneration || value.CardRevision != in.CardRevision || value.Kind != in.Kind || value.Message != in.Message || value.AcceptWithin != in.AcceptWithin {
		return false
	}
	if in.Dispatch == nil {
		return value.Dispatch == nil
	}
	if value.Dispatch == nil {
		return false
	}
	stored := *value.Dispatch
	if in.AcceptWithin != 0 {
		stored.CreatedAt, stored.ConfirmBy = time.Time{}, time.Time{}
	}
	left, _ := json.Marshal(stored)
	right, _ := json.Marshal(in.Dispatch)
	return string(left) == string(right)
}

func requestFailureKnown(err error) bool {
	var failure *api.Error
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.Code {
	case api.CodeInvalidRequest, api.CodeNotFound, api.CodeRevisionConflict, api.CodeHeldByOther, api.CodeLeaseInvalid, api.CodeLeaseActive, api.CodeClaimRequired, api.CodeEvidenceIncomplete, api.CodeProtocolIncompatible, api.CodeIdempotencyMismatch, api.CodeProjectMismatch, api.CodeProjectNotEmpty, api.CodeBoardRejected:
		return true
	}
	return false
}
