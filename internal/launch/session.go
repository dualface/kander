package launch

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/process"
)

var sessionReferenceRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func randomUUID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString(buf[:])
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func cursorCreateChat(program *process.AgentProgram) (string, error) {
	inv, err := newInvocation(*program, []string{"create-chat"}, nil)
	if err != nil {
		return "", launchError("launch.cursor_agent_create_chat_failed", err.Error())
	}
	cmd := exec.Command(inv.Argv[0], inv.Argv[1:]...)
	cmd.Env = envSlice(inv.Env)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()
	stdout, stderr := stdoutBuf.String(), stderrBuf.String()
	lines := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	chatID := ""
	if len(lines) > 0 {
		chatID = lines[len(lines)-1]
	}
	code := 0
	if runErr != nil {
		if ee, ok := runErr.(interface{ ExitCode() int }); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	if code != 0 || !sessionReferenceRe.MatchString(chatID) {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		if detail == "" {
			detail = "exit " + strconv.Itoa(code)
		}
		return "", launchError(
			"launch.cursor_agent_create_chat_did_not_return_a_usable", detail,
		)
	}
	return chatID, nil
}

func newAgentSession(agent string, program *process.AgentProgram) (AgentSession, error) {
	switch agent {
	case "claude", "grok":
		return AgentSession{Agent: agent, Reference: newUUID()}, nil
	case "cursor":
		id, err := cursorCreateChat(program)
		if err != nil {
			return AgentSession{}, err
		}
		return AgentSession{Agent: agent, Reference: id}, nil
	default:
		return AgentSession{Agent: agent}, nil
	}
}

func parseTaskSession(text string) *AgentSession {
	value := metadataFrom(text, sessionField)
	if value == "" {
		return nil
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return nil
	}
	agent := parts[0]
	reference := ""
	if len(parts) > 1 {
		reference = parts[1]
	}
	if !contains(config.ExecutionAgents, agent) || len(parts) > 2 || (reference != "" && !sessionReferenceRe.MatchString(reference)) {
		return nil
	}
	return &AgentSession{Agent: agent, Reference: reference}
}

func sessionFrom(text string) (AgentSession, error) {
	value := metadataFrom(text, sessionField)
	if value == "" {
		return AgentSession{}, launchError(
			"launch.task_has_no_metadata_only_tasks_launched_by_kander", sessionField,
		)
	}
	session := parseTaskSession(text)
	if session == nil {
		return AgentSession{}, launchError("launch.task_session_metadata_is_invalid", value)
	}
	return *session, nil
}

func resolvedTaskSession(taskID, text string) (AgentSession, error) {
	session, err := sessionFrom(text)
	if err != nil {
		return AgentSession{}, err
	}
	if session.Agent == "codex" && session.Reference == "" {
		id, err := findCodexSession(taskID)
		if err != nil {
			return AgentSession{}, err
		}
		return AgentSession{Agent: "codex", Reference: id}, nil
	}
	if session.Reference == "" {
		return AgentSession{}, launchError(
			"launch.task_session_for_has_no_id", session.Agent,
		)
	}
	return session, nil
}

func codexSessionsRoot() string {
	home := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, "sessions")
}

// Match both languages regardless of the current UI language. The Chinese heads
// also match sessions created before prompts were localized.
func promptPrefixes(taskID string, kinds ...string) []string {
	var out []string
	for _, lang := range []string{"cn", "en"} {
		for _, kind := range kinds {
			head := strings.TrimSuffix(i18n.Text(lang, "launch.prompt."+kind+"_head", taskID), ".")
			out = append(out, head+";", head+".")
		}
	}
	return out
}

func takeoverPromptPrefixes(taskID string) []string {
	return promptPrefixes(taskID, "takeover")
}

func codexPromptPrefixes(taskID string) []string {
	return promptPrefixes(taskID, "start", "resume", "takeover")
}

func startsWithAny(text string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

func codexRolloutMentionsTask(path, taskID string) string {
	prefixes := codexPromptPrefixes(taskID)
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.UseNumber()
	sessionID := ""
	mentioned := false
	for i := 0; i < 64; i++ {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			break
		}
		payload, _ := rec["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		typ, _ := rec["type"].(string)
		if typ == "session_meta" {
			if id, ok := payload["id"].(string); ok {
				sessionID = id
			}
		} else if typ == "response_item" {
			if role, _ := payload["role"].(string); role == "user" {
				content, _ := payload["content"].([]any)
				for _, item := range content {
					obj, _ := item.(map[string]any)
					text, _ := obj["text"].(string)
					if startsWithAny(text, prefixes) {
						mentioned = true
					}
				}
			}
		} else if typ == "event_msg" {
			if ptype, _ := payload["type"].(string); ptype == "user_message" {
				msg, _ := payload["message"].(string)
				if startsWithAny(msg, prefixes) {
					mentioned = true
				}
			}
		}
		if sessionID != "" && mentioned {
			return sessionID
		}
	}
	return ""
}

type fileInfo struct {
	path string
	mod  time.Time
}

func codexSessionsForTask(taskID string) ([]string, error) {
	root := codexSessionsRoot()
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, launchError(
			"launch.codex_sessions_directory_not_found_cannot_resume_with_context", root,
		)
	}
	var files []fileInfo
	_ = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl") {
			files = append(files, fileInfo{path: path, mod: fi.ModTime()})
		}
		return nil
	})
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].mod.After(files[i].mod) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	var sessions []string
	seen := map[string]struct{}{}
	for _, f := range files {
		id := codexRolloutMentionsTask(f.path, taskID)
		if id != "" && sessionReferenceRe.MatchString(id) {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				sessions = append(sessions, id)
			}
		}
	}
	return sessions, nil
}

func findCodexSession(taskID string) (string, error) {
	sessions, err := codexSessionsForTask(taskID)
	if err != nil {
		return "", err
	}
	if len(sessions) > 0 {
		return sessions[0], nil
	}
	root := codexSessionsRoot()
	return "", launchError(
		"launch.no_codex_execution_session_started_for_was_found_under", root, taskID,
	)
}

func discoverNewCodexSession(taskID string, previous map[string]struct{}) (string, error) {
	deadline := nowFn().Add(sessionDiscoverWait)
	var last error
	for {
		candidates, err := codexSessionsForTask(taskID)
		if err != nil {
			last = err
		} else {
			var neu []string
			for _, id := range candidates {
				if _, ok := previous[id]; !ok {
					neu = append(neu, id)
				}
			}
			if len(neu) == 1 {
				return neu[0], nil
			}
			if len(neu) > 1 {
				return "", launchError(
					"launch.multiple_new_codex_sessions_appeared_during_launch", strconv.Itoa(len(neu)),
				)
			}
			last = launchError("launch.the_newly_started_codex_session_has_not_appeared_yet")
		}
		if !nowFn().Before(deadline) {
			return "", last
		}
		sleepFn(notifyPollInterval)
	}
}

func agentArguments(agent string, model map[string]string, kind string, session AgentSession, resume bool) ([]string, error) {
	scale := "small"
	if kind == "large" {
		scale = "large"
	}
	// 模型按任务规模取, 规模模型为空时回落到旧配置的共享 "model" 键.
	modelID := config.KanbanModelFor(model, scale)
	if agent == "cursor" {
		var args []string
		if modelID != "" {
			args = append(args, "--model", modelID)
		}
		return append(args, "--trust", "--force", "--resume", session.Reference), nil
	}
	effortKey := scale + "_effort"
	effort := model[effortKey]
	var modelArgs []string
	if modelID != "" {
		modelArgs = []string{"--model", modelID}
	}
	switch agent {
	case "codex":
		options := append(append([]string{}, modelArgs...), "--config", `model_reasoning_effort="`+effort+`"`, "--dangerously-bypass-approvals-and-sandbox")
		if resume {
			return append(append([]string{"resume"}, options...), session.Reference), nil
		}
		return options, nil
	case "claude":
		flag := "--session-id"
		if resume {
			flag = "--resume"
		}
		return append(modelArgs, "--effort", effort, "--dangerously-skip-permissions", flag, session.Reference), nil
	case "grok":
		flag := "--session-id"
		if resume {
			flag = "--resume"
		}
		return append(modelArgs, "--effort", effort, "--permission-mode", "bypassPermissions", flag, session.Reference), nil
	default:
		return nil, launchError("launch.unsupported_agent", agent)
	}
}

func requireAgentProgram(agentName string) (*process.AgentProgram, error) {
	executable := config.AgentExecutableName(agentName)
	program := resolveAgent(executable)
	if program != nil {
		return program, nil
	}
	return nil, launchError("launch.agent_is_not_in_path", executable)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }
