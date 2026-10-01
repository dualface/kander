package launch

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

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
		id, err := readOpenSession(ctx, root, observed, task, cwd)
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

func readOpenSession(ctx context.Context, root string, observed process.OpenFile, task, cwd string) (string, error) {
	file, err := fs.OpenRegularFileIfExists(root, observed.Path)
	if err != nil || file == nil {
		return "", repairError("launch.session_repair_binding_missing")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || observed.Info == nil || !os.SameFile(info, observed.Info) {
		return "", repairError("launch.session_repair_changed")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	prefixes := codexPromptPrefixes(task)
	id := ""
	for index := range 64 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				ID      string `json:"id"`
				CWD     string `json:"cwd"`
				Type    string `json:"type"`
				Role    string `json:"role"`
				Message string `json:"message"`
				Content []struct {
					Text string `json:"text"`
					Type string `json:"type"`
				} `json:"content"`
			} `json:"payload"`
		}
		if err := decoder.Decode(&record); err != nil {
			return "", nil
		}
		if index == 0 {
			if record.Type != "session_meta" || !sessionReferenceRe.MatchString(record.Payload.ID) {
				return "", nil
			}
			if !matchesSessionDirectory(record.Payload.CWD, cwd) {
				return "", &sessionDirectoryMismatch{repairError("launch.session_repair_directory_mismatch")}
			}
			id = record.Payload.ID
			continue
		}
		if record.Type == "event_msg" && record.Payload.Type == "user_message" && startsWithAny(record.Payload.Message, prefixes) {
			return id, nil
		}
		if record.Type == "response_item" && record.Payload.Role == "user" {
			for _, content := range record.Payload.Content {
				if (content.Type == "input_text" || content.Type == "text") && startsWithAny(content.Text, prefixes) {
					return id, nil
				}
			}
		}
	}
	return "", nil
}
