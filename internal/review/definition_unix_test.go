//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

const fakeCustomReviewer = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_CUSTOM_ARGV"
if [ -n "${FAKE_CUSTOM_STDIN_LOG:-}" ]; then
    if [ -t 0 ]; then
        printf 'tty\n' > "$FAKE_CUSTOM_STDIN_LOG"
    else
        cat > "$FAKE_CUSTOM_STDIN_LOG"
    fi
fi
pwd -P > "$FAKE_CUSTOM_CWD"
if [ -n "${FAKE_CUSTOM_PROMPT_COPY:-}" ] && [ -n "$1" ]; then
    :
fi
i=0
prompt=""
instruction=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --file) prompt="$2"; shift ;;
        --out) out="$2"; shift ;;
    esac
    instruction="$1"
    shift
done
if [ -n "$prompt" ] && [ -n "${FAKE_CUSTOM_PROMPT_COPY:-}" ]; then
    cp "$prompt" "$FAKE_CUSTOM_PROMPT_COPY"
    ls -l "$prompt" | awk '{print $1}' > "$FAKE_CUSTOM_PROMPT_MODE"
fi
case "${FAKE_CUSTOM_MODE:-file}" in
    file)
        printf '%s\n' "${FAKE_CUSTOM_REPORT:-FILE REPORT}" > "${out:-$FAKE_CUSTOM_OUT}"
        ;;
    stdout-json)
        printf '{"result":"%s"}\n' "${FAKE_CUSTOM_REPORT:-JSON REPORT}"
        ;;
    stdout-regex)
        printf 'BEGIN %s END\n' "${FAKE_CUSTOM_REPORT:-REGEX REPORT}"
        ;;
    stdout-ndjson)
        printf '%s\n' \
            '{"role":"tool","content":"tool noise"}' \
            '{"role":"meta","content":"session.resume_hint"}' \
            '{"role":"assistant","content":"line one"}' \
            '{"role":"assistant","content":"line two"}'
        ;;
esac
exit 0
`

func writeCustomReviewerConfig(t *testing.T, h *reviewHarness, def map[string]any) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "foreground"
	payload, _ := json.Marshal(cfg)
	var root map[string]any
	json.Unmarshal(payload, &root)
	root["agents"] = map[string]any{"helper": def}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(config.EnvConfig), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(false); err != nil {
		t.Fatal(err)
	}
}

func newCustomHarness(t *testing.T) *reviewHarness {
	t.Helper()
	root := t.TempDir()
	h := &reviewHarness{t: t, root: root, repo: filepath.Join(root, "repo"), tmp: filepath.Join(root, "tmp"), home: filepath.Join(root, "home")}
	for _, p := range []string{h.repo, h.tmp, h.home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.fake = filepath.Join(root, "fake-helper")
	h.argvLog = filepath.Join(root, "argv.log")
	h.cwdLog = filepath.Join(root, "cwd.log")
	h.promptLog = filepath.Join(root, "prompt.copy")
	writeFake(t, h.fake, fakeCustomReviewer)
	gitRepo(t, h.repo, "init", "-q", "-b", "main")
	h.base = commitFile(t, h.repo, "a.txt", "base\n", "基线")
	h.head = commitFile(t, h.repo, "b.txt", "head\n", "改动")
	real, _ := filepath.EvalSymlinks(h.repo)
	h.repoReal = real
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("TMPDIR", h.tmp)
	t.Setenv("TMP", h.tmp)
	t.Setenv("TEMP", h.tmp)
	t.Setenv("HOME", h.home)
	setupLang(t, filepath.Join(root, "kander-config.json"))
	t.Setenv("FAKE_CUSTOM_ARGV", h.argvLog)
	t.Setenv("FAKE_CUSTOM_CWD", h.cwdLog)
	t.Setenv("HELPER_REVIEW_CHECK_INTERVAL_SECONDS", "1")
	t.Setenv("HELPER_REVIEW_MAX_RUNTIME_SECONDS", "30")
	return h
}

func TestCustomReviewerOutputSources(t *testing.T) {
	h := newCustomHarness(t)
	t.Setenv("FAKE_CUSTOM_OUT", filepath.Join(h.root, "forced-out.txt"))
	cases := []struct {
		name   string
		mode   string
		output map[string]any
		want   string
	}{
		{"file-raw", "file", map[string]any{"source": "file", "parse": "raw"}, "FILE REPORT"},
		{"stdout-json", "stdout-json", map[string]any{"source": "stdout", "parse": "json_field:result"}, "JSON REPORT"},
		{"stdout-regex", "stdout-regex", map[string]any{"source": "stdout", "parse": "regex:BEGIN (.*) END"}, "REGEX REPORT"},
		{"stdout-ndjson", "stdout-ndjson", map[string]any{
			"source": "stdout", "format": "ndjson",
			"select": []any{map[string]any{"json_field": "role", "equals": "assistant"}},
			"parse":  "json_field:content", "join": "\n",
		}, "line one\nline two"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Setenv("FAKE_CUSTOM_MODE", item.mode)
			writeCustomReviewerConfig(t, h, map[string]any{
				"path": h.fake,
				"args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--out", "{output}"}},
				"session": map[string]any{"mode": "none"},
				"review": map[string]any{
					"cwd": "runtime", "output_name": "result.txt", "inspection": "INSPECT-TOKEN",
					"home_policy": "optional", "output": item.output,
				},
			})
			code, out, err := h.review("helper", "QA", "确认改动正确")
			if code != 0 {
				t.Fatalf("code=%d err=%s out=%s", code, err, out)
			}
			if !strings.Contains(out, item.want) {
				t.Fatalf("out=%q", out)
			}
		})
	}
}

func TestCustomReviewerInspectionInPrompt(t *testing.T) {
	h := newCustomHarness(t)
	t.Setenv("FAKE_CUSTOM_MODE", "file")
	t.Setenv("FAKE_CUSTOM_PROMPT_COPY", h.promptLog)
	writeCustomReviewerConfig(t, h, map[string]any{
		"path": h.fake,
		"args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--file", "{prompt_file}", "--out", "{output}"}},
		"session": map[string]any{"mode": "none"},
		"review": map[string]any{
			"cwd": "runtime", "output_name": "result.txt", "inspection": "INSPECT-TOKEN",
			"home_policy": "optional", "output": map[string]any{"source": "file", "parse": "raw"},
		},
	})
	code, _, err := h.review("helper", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, err)
	}
	if !strings.Contains(readFile(t, h.promptLog), "INSPECT-TOKEN") {
		t.Fatalf("prompt=%s", readFile(t, h.promptLog))
	}
}

func TestCustomReviewerStdinNonePromptFiles(t *testing.T) {
	h := newCustomHarness(t)
	t.Setenv("FAKE_CUSTOM_MODE", "file")
	t.Setenv("FAKE_CUSTOM_STDIN_LOG", filepath.Join(h.root, "stdin.log"))
	t.Setenv("FAKE_CUSTOM_PROMPT_COPY", h.promptLog)
	t.Setenv("FAKE_CUSTOM_PROMPT_MODE", filepath.Join(h.root, "prompt.mode"))
	template := "INSPECT={inspection}\nPROMPT={prompt}\n"
	writeCustomReviewerConfig(t, h, map[string]any{
		"path": h.fake,
		"args": map[string]any{
			"start": []string{}, "resume": []string{},
			"review": []string{"--file", "{prompt_file:guide}", "{instruction}", "--out", "{output}"},
		},
		"session": map[string]any{"mode": "none"},
		"review": map[string]any{
			"cwd": "runtime", "output_name": "result.txt", "inspection": "NO-WRITE",
			"home_policy": "optional", "stdin": "none",
			"output": map[string]any{"source": "file", "parse": "raw"},
			"prompt_files": []any{map[string]any{"name": "guide", "path": "guide.md", "template": template}},
		},
	})
	code, out, err := h.review("helper", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("code=%d err=%s out=%s", code, err, out)
	}
	rendered := readFile(t, h.promptLog)
	if !strings.HasPrefix(rendered, "INSPECT=NO-WRITE\nPROMPT=") {
		t.Fatalf("rendered=%q", rendered)
	}
	if !strings.Contains(rendered, "You are the QA review agent") {
		t.Fatalf("prompt missing body: %s", rendered)
	}
	mode := strings.TrimSpace(readFile(t, filepath.Join(h.root, "prompt.mode")))
	if mode != "-rw-------" && !strings.Contains(mode, "rw-------") {
		t.Fatalf("mode=%q", mode)
	}
	stdin := readFile(t, filepath.Join(h.root, "stdin.log"))
	if strings.Contains(stdin, "Perform the QA review") {
		t.Fatalf("stdin should be empty, got %q", stdin)
	}
	argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
	if !contains(argv, "--file") {
		t.Fatalf("argv=%v", argv)
	}
}

func TestCustomAndBuiltinHomePolicy(t *testing.T) {
	h := newCustomHarness(t)
	t.Setenv("FAKE_CUSTOM_MODE", "file")
	missing := filepath.Join(h.root, "absent-home")
	t.Setenv("HELPER_HOME", missing)
	writeCustomReviewerConfig(t, h, map[string]any{
		"path": h.fake,
		"args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--out", "{output}"}},
		"session": map[string]any{"mode": "none"},
		"review": map[string]any{
			"cwd": "runtime", "output_name": "result.txt", "home_env": "HELPER_HOME",
			"home_policy": "optional", "output": map[string]any{"source": "file", "parse": "raw"},
		},
	})
	code, _, err := h.review("helper", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("optional code=%d err=%s", code, err)
	}
	writeCustomReviewerConfig(t, h, map[string]any{
		"path": h.fake,
		"args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--out", "{output}"}},
		"session": map[string]any{"mode": "none"},
		"review": map[string]any{
			"cwd": "runtime", "output_name": "result.txt", "home_env": "HELPER_HOME",
			"home_policy": "required", "output": map[string]any{"source": "file", "parse": "raw"},
		},
	})
	code, _, err = h.review("helper", "QA", "确认改动正确")
	if code != 2 || !strings.Contains(err, "not readable and writable") {
		t.Fatalf("required code=%d err=%q", code, err)
	}
}

func TestCodexNonZeroExitRejectsReportText(t *testing.T) {
	h := newCodexHarness(t)
	writeFake(t, h.fake, `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "--output-last-message" ]; then
        out="$2"
    fi
    shift
done
if [ -n "$out" ]; then
    printf '%s\n' "REPORT BODY" > "$out"
fi
printf '%s\n' 'fake codex failure' >&2
exit 3
`)
	code, out, err := h.defaultReview()
	if code != 3 || !strings.Contains(err, "fake codex failure") {
		t.Fatalf("code=%d err=%q", code, err)
	}
	if !strings.Contains(out, "REPORT BODY") {
		t.Fatalf("out=%q", out)
	}
}

func TestBuiltinReviewStdinAndPromptBytes(t *testing.T) {
	for _, agent := range []string{"codex", "claude", "cursor", "grok"} {
		t.Run(agent, func(t *testing.T) {
			var h *reviewHarness
			var promptPath string
			switch agent {
			case "codex":
				h = newCodexHarness(t)
				promptPath = h.stdinLog
			case "claude":
				h = newClaudeHarness(t)
				promptPath = h.promptLog
			case "cursor":
				h = newCursorHarness(t)
				promptPath = h.promptLog
			case "grok":
				h = newGrokHarness(t)
				promptPath = h.promptLog
			}
			code, _, err := h.review(agent, "QA", "确认改动正确")
			if code != 0 {
				t.Fatalf("code=%d err=%s", code, err)
			}
			prompt := readFile(t, promptPath)
			if !strings.Contains(prompt, "You are the QA review agent") {
				t.Fatalf("prompt missing body: %s", prompt)
			}
			wantInspection := map[string]string{
				"codex":  "Use only read-only filesystem and shell operations needed to inspect code.",
				"claude": "Use only read-only inspection: read, search, and shell commands that do not write.",
				"cursor": "Prefer read-only inspection.",
				"grok":   "Use only read_file, grep, and list_dir to inspect code.",
			}[agent]
			if !strings.Contains(prompt, wantInspection) {
				t.Fatalf("prompt missing inspection: %s", prompt)
			}
			if agent == "grok" {
				argv := strings.Split(strings.TrimRight(readFile(t, h.argvLog), "\n"), "\n")
				if !contains(argv, "--prompt-file") {
					t.Fatalf("argv=%v", argv)
				}
			} else {
				instruction := process.TaskFileInstruction("Perform the QA review.", "dummy")
				prefix, _, _ := strings.Cut(instruction, "dummy")
				if prefix == "" || !strings.Contains(prefix, "Perform the QA review") {
					t.Fatalf("instruction %q", instruction)
				}
			}
		})
	}
}

func TestBuiltinHomePolicyClaudeVsCursor(t *testing.T) {
	claude := newClaudeHarness(t)
	missing := filepath.Join(claude.root, "absent-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", missing)
	code, _, err := claude.review("claude", "QA", "确认改动正确")
	if code != 2 || !strings.Contains(err, "not readable and writable") {
		t.Fatalf("claude missing home code=%d err=%q", code, err)
	}
	cursor := newCursorHarness(t)
	absent := filepath.Join(cursor.root, "absent-cursor-home")
	t.Setenv("CURSOR_CONFIG_DIR", absent)
	code, _, err = cursor.review("cursor", "QA", "确认改动正确")
	if code != 0 {
		t.Fatalf("cursor optional home code=%d err=%s", code, err)
	}
}
