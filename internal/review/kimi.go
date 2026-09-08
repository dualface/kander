package review

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// kimi-code has no command-line switch for permissions, sandboxing or tool selection, and
// its --plan read-only mode cannot be combined with --prompt. The only per-invocation lever
// is config.toml, which lives under KIMI_CODE_HOME. Pointing that variable at an empty
// directory would also discard the OAuth credentials the reviewer needs, so the review runs
// against a private copy of the home: the user's own config plus deny rules appended, and
// the credentials carried over. The copy lives in the review runtime and dies with it.
const (
	kimiHomeDir    = "kimi-home"
	kimiEffortEnv  = "KIMI_MODEL_THINKING_EFFORT"
	kimiHomeEnv    = "KIMI_CODE_HOME"
	kimiCredential = "credentials"
)

// kimiDenyRules blocks every mutating and outbound tool. Rules are evaluated top down and the
// first match wins, so the denials precede the allowances.
const kimiDenyRules = `

# Appended by Kander: read-only review isolation.
[[permission.rules]]
decision = "deny"
pattern = "Bash"

[[permission.rules]]
decision = "deny"
pattern = "Write"

[[permission.rules]]
decision = "deny"
pattern = "Edit"

[[permission.rules]]
decision = "deny"
pattern = "WebSearch"

[[permission.rules]]
decision = "deny"
pattern = "FetchURL"

[[permission.rules]]
decision = "deny"
pattern = "Agent"

[[permission.rules]]
decision = "deny"
pattern = "AgentSwarm"

[[permission.rules]]
decision = "deny"
pattern = "Skill"

[[permission.rules]]
decision = "deny"
pattern = "CronCreate"

[[permission.rules]]
decision = "deny"
pattern = "CronDelete"

[[permission.rules]]
decision = "allow"
pattern = "Read"

[[permission.rules]]
decision = "allow"
pattern = "Grep"

[[permission.rules]]
decision = "allow"
pattern = "Glob"
`

// prepareKimiHome builds the private KIMI_CODE_HOME for one review and returns its path.
func prepareKimiHome(runtime, sourceHome string) (string, error) {
	home := filepath.Join(runtime, kimiHomeDir)
	if err := fs.CreatePrivateDirectory(runtime, home); err != nil {
		return "", newGate(2, "review.could_not_create_the_private_review_runtime", err.Error())
	}
	// A missing or unreadable source config is not fatal: kimi-code falls back to its own
	// defaults, and the deny rules still apply because they are written unconditionally.
	base, _, err := fs.ReadRegularFileIfExists(sourceHome, filepath.Join(sourceHome, "config.toml"))
	if err != nil {
		base = nil
	}
	text := strings.TrimRight(string(base), "\r\n") + kimiDenyRules
	if err := fs.WriteTextAtomic(runtime, filepath.Join(home, "config.toml"), text, false); err != nil {
		return "", newGate(2, "review.review_runtime_i_o_failed", runtime, err.Error())
	}
	if err := copyKimiCredentials(runtime, sourceHome, home); err != nil {
		return "", err
	}
	return home, nil
}

// copyKimiCredentials carries the stored OAuth tokens into the private home. Without them the
// reviewer cannot authenticate; the copies are private files inside the runtime and are
// removed with it when the review ends.
func copyKimiCredentials(runtime, sourceHome, home string) error {
	source := filepath.Join(sourceHome, kimiCredential)
	entries, err := fs.ListDirectory(sourceHome, source)
	if err != nil {
		// No credentials directory: the reviewer may still authenticate from an API key in
		// config.toml, so let the run proceed and report a real failure if it cannot.
		return nil
	}
	target := filepath.Join(home, kimiCredential)
	if err := fs.CreatePrivateDirectory(home, target); err != nil {
		return newGate(2, "review.review_runtime_i_o_failed", runtime, err.Error())
	}
	for _, entry := range entries {
		if entry.Kind != fs.KindFile {
			continue
		}
		data, err := fs.ReadRegularFile(sourceHome, filepath.Join(source, entry.Name))
		if err != nil {
			continue
		}
		path := filepath.Join(target, entry.Name)
		if err := fs.WriteBytesAtomicInherited(home, path, data, false); err != nil {
			return newGate(2, "review.review_runtime_i_o_failed", runtime, err.Error())
		}
		if err := fs.TightenPrivateFile(path); err != nil {
			return newGate(2, "review.review_runtime_i_o_failed", runtime, err.Error())
		}
	}
	return nil
}

// kimiReviewText reconstructs the report from a kimi-code stream-json transcript. Every line is
// one OpenAI-shaped message; the report is the concatenation of the assistant turns. A tool
// call leaves content null, and the trailing meta line carries a resume hint that is not part
// of the report, so both are filtered out by role rather than by content.
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
