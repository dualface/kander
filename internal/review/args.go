package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

func flattenEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func reviewerArguments(ctx reviewContext, runtime, outputFile, promptFile string) (process.ProcessInvocation, string, error) {
	settings := ctx.settings
	environment := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			environment[kv] = ""
			continue
		}
		environment[k] = v
	}
	environment["GIT_OPTIONAL_LOCKS"] = "0"
	var model []string
	if settings.model != "" {
		model = []string{"--model", settings.model}
	}
	home, err := filepath.Abs(settings.reviewHome)
	if err != nil {
		home = settings.reviewHome
	}
	if resolved, resErr := filepath.EvalSymlinks(home); resErr == nil {
		home = resolved
	}
	var arguments []string
	cwd := runtime
	switch ctx.agent {
	case "codex":
		environment["CODEX_HOME"] = home
		arguments = append([]string{"exec", "--cd", ctx.root}, model...)
		arguments = append(arguments,
			"--sandbox", "read-only",
			"--ephemeral",
			"--config", `model_reasoning_effort="`+settings.effort+`"`,
			"--config", `web_search="live"`,
			"--config", "allow_login_shell=false",
			"--output-last-message", outputFile,
			"-",
		)
		cwd = ctx.root
	case "claude":
		environment["CLAUDE_CONFIG_DIR"] = home
		arguments = append([]string{
			"--print", "--output-format", "json",
			"--permission-mode", "plan",
			"--tools", "Read,Grep,Glob",
			"--disallowedTools", "Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch,Task,TaskOutput,TaskStop,EnterPlanMode,ExitPlanMode,AskUserQuestion",
			"--add-dir", ctx.root,
			"--safe-mode",
			"--disable-slash-commands",
			"--no-session-persistence",
		}, model...)
		arguments = append(arguments, "--effort", settings.effort)
		cwd = runtime
	case "cursor":
		environment["CURSOR_CONFIG_DIR"] = runtime
		environment["CURSOR_DATA_DIR"] = runtime
		arguments = append([]string{
			"--print", "--output-format", "json", "--trust",
			"--add-dir", ctx.root,
		}, model...)
		cwd = runtime
	case "grok":
		environment["GROK_HOME"] = home
		arguments = append([]string{"--cwd", runtime}, model...)
		arguments = append(arguments,
			"--effort", settings.effort,
			"--output-format", "json",
			"--permission-mode", "dontAsk",
			"--allow", "Read",
			"--allow", "Grep",
			"--tools", "read_file,grep,list_dir",
			"--disallowed-tools", "Agent,run_terminal_command,search_tool,use_tool,web_search,web_fetch,search_replace,todo_write,scheduler_create,scheduler_delete,scheduler_list,monitor,workflow,enter_plan_mode,exit_plan_mode,ask_user_question,image_gen,image_edit,image_to_video,reference_to_video,write",
			"--deny", "Edit",
			"--deny", "Write",
			"--deny", "MCPTool(*)",
			"--sandbox", "read-only",
			"--disable-web-search",
			"--no-memory",
			"--no-subagents",
			"--no-plan",
			"--verbatim",
			"--prompt-file", promptFile,
		)
		cwd = ctx.root
	default:
		return process.ProcessInvocation{}, "", newGate(2,
			"review.unsupported_reviewer_agent", ctx.agent,
		)
	}
	inv, err := process.NewProcessInvocation(ctx.program, arguments, environment)
	if err != nil {
		return process.ProcessInvocation{}, "", err
	}
	return inv, cwd, nil
}

// syncStream only syncs regular files. On Windows, FlushFileBuffers on the write end of a pipe blocks until the read
// end has drained everything, and it is meaningless on a console handle; os.File writes are unbuffered anyway, so no extra flush is needed.
func syncStream(stream *os.File) {
	info, err := stream.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	_ = stream.Sync()
}

func printFile(runtime, path string, stream *os.File) {
	data, err := fs.ReadRegularFile(runtime, path)
	if err != nil {
		return
	}
	_, _ = stream.Write(data)
	syncStream(stream)
}

func printErrorTail(runtime, path string) {
	data, err := fs.ReadRegularFile(runtime, path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return
	}
	start := 0
	if len(lines) > 10 {
		start = len(lines) - 10
	}
	fmt.Fprintln(os.Stderr, strings.Join(lines[start:], "\n"))
}

func parseReviewOutput(ctx reviewContext, runtime, outputFile, stdoutFile string) error {
	if ctx.agent == "codex" {
		data, err := fs.ReadRegularFile(runtime, outputFile)
		if err != nil || len(data) == 0 {
			printFile(runtime, stdoutFile, os.Stdout)
			return newGate(1, "review.codex_review_did_not_complete_with_review_text")
		}
		_, _ = os.Stdout.Write(data)
		syncStream(os.Stdout)
		return nil
	}
	raw, err := fs.ReadRegularFile(runtime, outputFile)
	var result any
	if err == nil {
		_ = json.Unmarshal(raw, &result)
	}
	var text string
	var valid bool
	var message string
	if ctx.agent == "grok" {
		obj, ok := result.(map[string]any)
		if ok {
			text, _ = obj["text"].(string)
			valid = obj["stopReason"] == "end_turn" && text != ""
		}
		message = config.Text("review.grok_review_did_not_complete_with_review_text")
	} else if ctx.agent == "claude" || ctx.agent == "cursor" {
		obj, ok := result.(map[string]any)
		if ok {
			text, _ = obj["result"].(string)
			valid = obj["type"] == "result" && obj["subtype"] == "success" && obj["is_error"] == false && text != ""
		}
		message = config.Text(
			"review.review_did_not_complete_with_review_text", ctx.settings.name,
		)
	} else {
		printFile(runtime, outputFile, os.Stdout)
		return newGate(1, "review.unsupported_reviewer_agent", ctx.agent)
	}
	if !valid {
		printFile(runtime, outputFile, os.Stdout)
		return newGateMsg(1, message)
	}
	fmt.Println(text)
	return nil
}

func copySpecSnapshot(src, runtime, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := fs.WriteTextAtomic(runtime, dest, string(data), false); err != nil {
		return err
	}
	return fs.MakeRegularFileReadOnly(runtime, dest)
}
