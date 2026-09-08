package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// kimiEffortEnv carries the reasoning effort into a kimi-code run. kimi-code has no
// command-line switch for it; the variable is the only per-invocation channel that does
// not require rewriting the user's own config.toml.
const kimiEffortEnv = "KIMI_MODEL_THINKING_EFFORT"

// kimi-code mints its own session id and never accepts one from the caller, so a session
// is recovered the way a Codex rollout is: by finding the transcript whose first user
// prompt is the Kander task prompt. The store is one directory per session,
// <root>/sessions/<work-dir-key>/<session-id>/, with the transcript in agents/main/wire.jsonl.
const kimiTranscriptScanRecords = 64

func kimiSessionsRoot() string {
	home := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".kimi-code")
	}
	return filepath.Join(home, "sessions")
}

// kimiSessionMentionsTask returns the session id when the transcript in dir opens with a
// Kander prompt for taskID, and the empty string otherwise. The id is the directory name.
func kimiSessionMentionsTask(dir, taskID string) string {
	id := filepath.Base(dir)
	if !sessionReferenceRe.MatchString(id) {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, "agents", "main", "wire.jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()
	prefixes := launchPromptPrefixes(taskID)
	dec := json.NewDecoder(f)
	dec.UseNumber()
	for i := 0; i < kimiTranscriptScanRecords; i++ {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			break
		}
		if typ, _ := rec["type"].(string); typ != "turn.prompt" {
			continue
		}
		origin, _ := rec["origin"].(map[string]any)
		if kind, _ := origin["kind"].(string); kind != "user" {
			continue
		}
		input, _ := rec["input"].([]any)
		for _, item := range input {
			obj, _ := item.(map[string]any)
			text, _ := obj["text"].(string)
			if startsWithAny(text, prefixes) {
				return id
			}
		}
	}
	return ""
}

// kimiSessionsForTask lists the ids of kimi-code sessions started from a Kander prompt for
// taskID, most recently touched first.
func kimiSessionsForTask(taskID string) ([]string, error) {
	root := kimiSessionsRoot()
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, launchError(
			"launch.kimi_sessions_directory_not_found_cannot_resume_with_context", root,
		)
	}
	workDirs, err := os.ReadDir(root)
	if err != nil {
		return nil, launchError(
			"launch.kimi_sessions_directory_not_found_cannot_resume_with_context", root,
		)
	}
	var dirs []fileInfo
	for _, workDir := range workDirs {
		if !workDir.IsDir() {
			continue
		}
		parent := filepath.Join(root, workDir.Name())
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(parent, entry.Name())
			// A session that never recorded a turn has no transcript to match against.
			stat, err := os.Stat(filepath.Join(path, "agents", "main", "wire.jsonl"))
			if err != nil {
				continue
			}
			dirs = append(dirs, fileInfo{path: path, mod: stat.ModTime()})
		}
	}
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			if dirs[j].mod.After(dirs[i].mod) {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
		}
	}
	var sessions []string
	seen := map[string]struct{}{}
	for _, d := range dirs {
		id := kimiSessionMentionsTask(d.path, taskID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			sessions = append(sessions, id)
		}
	}
	return sessions, nil
}

func findKimiSession(taskID string) (string, error) {
	sessions, err := kimiSessionsForTask(taskID)
	if err != nil {
		return "", err
	}
	if len(sessions) > 0 {
		return sessions[0], nil
	}
	return "", launchError(
		"launch.no_kimi_execution_session_started_for_was_found_under", kimiSessionsRoot(), taskID,
	)
}
