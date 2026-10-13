package launch

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

func proveOpenSession(ctx context.Context, files process.ProcessFiles, task, reference, cwd string) (sessionProof, error) {
	root, err := filepath.Abs(codexSessionsRoot())
	if err != nil || files.Identity == "" {
		return sessionProof{}, repairError("launch.session_repair_binding_missing")
	}
	var proof sessionProof
	seen := make(map[string]bool)
	prefixes := codexPromptPrefixes(task)
	for _, observed := range files.Files {
		if err := ctx.Err(); err != nil {
			return sessionProof{}, err
		}
		path := observed.Path
		base := filepath.Base(path)
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsAbs(path) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || !strings.HasPrefix(base, "rollout-") || !strings.HasSuffix(base, ".jsonl") || seen[path] {
			continue
		}
		seen[path] = true
		id, err := readOpenSession(ctx, root, observed, prefixes, cwd)
		if err != nil {
			return sessionProof{}, err
		}
		if id == "" {
			continue
		}
		if proof.reference != "" {
			return sessionProof{}, repairError("launch.session_repair_binding_ambiguous")
		}
		proof = sessionProof{reference: id, process: files.Identity, path: path, info: observed.Info}
	}
	if proof.reference == "" {
		return sessionProof{}, repairError("launch.session_repair_binding_missing")
	}
	if reference != "" && proof.reference != reference {
		return sessionProof{}, repairError("launch.session_repair_binding_mismatch")
	}
	return proof, nil
}

// rolloutSession is what one rollout file says about its session.
type rolloutSession struct {
	// id is empty when the session_meta header is missing, invalid, or not fully written yet.
	id string
	// created is the session creation time; zero when the header carries no valid timestamp.
	created time.Time
	// bound reports whether a user message begins with one of the task prompt prefixes.
	bound bool
}

// readOpenSession returns the session id of an observed rollout bound to the
// task prompt prefixes and launch directory, or "" when it is not bound.
func readOpenSession(ctx context.Context, root string, observed process.OpenFile, prefixes []string, cwd string) (string, error) {
	session, err := readRolloutSession(ctx, root, observed, prefixes, cwd)
	if err != nil || !session.bound {
		return "", err
	}
	return session.id, nil
}

// readRolloutSession reads at most 64 records / 1 MiB of an observed rollout.
// A header for another launch directory returns a *sessionDirectoryMismatch.
func readRolloutSession(ctx context.Context, root string, observed process.OpenFile, prefixes []string, cwd string) (rolloutSession, error) {
	file, err := fs.OpenRegularFileIfExists(root, observed.Path)
	if err != nil || file == nil {
		return rolloutSession{}, repairError("launch.session_repair_binding_missing")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || observed.Info == nil || !os.SameFile(info, observed.Info) {
		return rolloutSession{}, repairError("launch.session_repair_changed")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	var session rolloutSession
	for index := range 64 {
		if err := ctx.Err(); err != nil {
			return rolloutSession{}, err
		}
		var record struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID        string `json:"id"`
				CWD       string `json:"cwd"`
				Timestamp string `json:"timestamp"`
				Type      string `json:"type"`
				Role      string `json:"role"`
				Message   string `json:"message"`
				Content   []struct {
					Text string `json:"text"`
					Type string `json:"type"`
				} `json:"content"`
			} `json:"payload"`
		}
		if err := decoder.Decode(&record); err != nil {
			return session, nil
		}
		if index == 0 {
			if record.Type != "session_meta" || !sessionReferenceRe.MatchString(record.Payload.ID) {
				return rolloutSession{}, nil
			}
			if !matchesSessionDirectory(record.Payload.CWD, cwd) {
				return rolloutSession{}, &sessionDirectoryMismatch{repairError("launch.session_repair_directory_mismatch")}
			}
			session.id = record.Payload.ID
			session.created = rolloutCreatedAt(record.Payload.Timestamp, record.Timestamp)
			continue
		}
		if record.Type == "event_msg" && record.Payload.Type == "user_message" && startsWithAny(record.Payload.Message, prefixes) {
			session.bound = true
			return session, nil
		}
		if record.Type == "response_item" && record.Payload.Role == "user" {
			for _, content := range record.Payload.Content {
				if (content.Type == "input_text" || content.Type == "text") && startsWithAny(content.Text, prefixes) {
					session.bound = true
					return session, nil
				}
			}
		}
	}
	return session, nil
}

// rolloutCreatedAt parses the session creation time of a session_meta header.
// The payload timestamp is the creation time; the record timestamp is when the
// header was written and only stands in when the payload has none.
func rolloutCreatedAt(payload, record string) time.Time {
	for _, value := range []string{payload, record} {
		if value == "" {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}
		}
		return created
	}
	return time.Time{}
}
