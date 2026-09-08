package review

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// kimi-code takes no sandbox, permission or tool flags, and its read-only --plan mode cannot be
// combined with the --prompt mode a review runs in. Isolation therefore comes from an agent
// definition written for the round: its frontmatter tool allowlist is the only surface the
// model is given, so the mutating and outbound tools are never exposed rather than merely
// refused. The file lives in the review runtime and dies with it, which leaves KIMI_CODE_HOME
// pointing at the reviewer's real home the way the Codex, Claude and Grok adapters do, so the
// stored credentials keep working without being copied anywhere.
const (
	kimiHomeEnv       = "KIMI_CODE_HOME"
	kimiAgentFileName = "kander-reviewer.md"
)

// kimiReviewTools is the allowlist. Anything absent — Bash, Write, Edit, WebSearch, FetchURL,
// Agent, Skill, and every MCP tool — is unavailable to the reviewer.
const kimiReviewTools = "[Read, Grep, Glob]"

// writeKimiReviewerAgent writes the round's agent definition and returns its path. The body
// replaces the default system prompt, so it has to state the reviewer's role in full.
func writeKimiReviewerAgent(runtime, inspection string) (string, error) {
	path := filepath.Join(runtime, kimiAgentFileName)
	body := strings.Join([]string{
		"---",
		"name: kander-reviewer",
		"description: Read-only code reviewer used by Kander.",
		"tools: " + kimiReviewTools,
		"---",
		"",
		"You are a code reviewer. You inspect a worktree and report findings; you never change it.",
		"",
		inspection,
		"",
		"Read the task file you are pointed at completely before reviewing, and report against it.",
		"",
	}, "\n")
	if err := fs.WriteTextAtomic(runtime, path, body, false); err != nil {
		return "", newGate(2, "review.review_runtime_i_o_failed", runtime, err.Error())
	}
	return path, nil
}

// kimiReviewText reconstructs the report from a kimi-code stream-json transcript. Every line is
// one OpenAI-shaped message; the report is the concatenation of the assistant turns. A tool
// call leaves content null, and the meta lines carry a version banner and a resume hint that
// are not part of the report, so both are filtered out by role rather than by content.
func kimiReviewText(raw []byte) (string, bool) {
	var parts []string
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var message struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		if message.Role != "assistant" {
			continue
		}
		if text, ok := message.Content.(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	return text, text != ""
}

// kimiReviewMessage names the reviewer in the shared "did not complete" diagnostic.
func kimiReviewMessage(name string) string {
	return config.Text("review.review_did_not_complete_with_review_text", name)
}
