package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/process"
)

// repairRollout writes a rollout created an hour ago, before any launch a test
// starts, so discovery treats it as a pre-existing session.
func repairRollout(t *testing.T, task, name, id, prompt string) process.OpenFile {
	t.Helper()
	return codexRolloutAt(t, task, name, id, prompt, time.Now().Add(-time.Hour))
}

// codexRolloutAt writes rollout-<name>.jsonl into the UTC date directory of
// created, with created as the session_meta timestamp. An empty prompt uses
// this task's start prompt.
func codexRolloutAt(t *testing.T, task, name, id, prompt string, created time.Time) process.OpenFile {
	t.Helper()
	root := filepath.Join(codexSessionsRoot(), filepath.FromSlash(created.UTC().Format("2006/01/02")))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "rollout-"+name+".jsonl")
	if prompt == "" {
		prompt = codexPromptPrefixes(task)[0] + " full instructions"
	}
	records := []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": filepath.Dir(os.Getenv(board.EnvBoardDir)), "timestamp": created.UTC().Format(time.RFC3339Nano)}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}},
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := json.NewEncoder(file).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return process.OpenFile{Path: path, Info: info}
}

func rewriteRolloutDirectory(t *testing.T, observed process.OpenFile, cwd string) process.OpenFile {
	t.Helper()
	data, err := os.ReadFile(observed.Path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(string(data), "\n", 2)
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	payload := header["payload"].(map[string]any)
	if cwd == "" {
		delete(payload, "cwd")
	} else {
		payload["cwd"] = cwd
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(observed.Path, []byte(string(encoded)+"\n"+lines[1]), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(observed.Path)
	if err != nil {
		t.Fatal(err)
	}
	return process.OpenFile{Path: observed.Path, Info: info}
}
