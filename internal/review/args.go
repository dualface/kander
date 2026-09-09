package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

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
	home, err := filepath.Abs(settings.reviewHome)
	if err != nil {
		home = settings.reviewHome
	}
	if resolved, resErr := filepath.EvalSymlinks(home); resErr == nil {
		home = resolved
	}
	values := map[string]string{
		"model":       settings.model,
		"effort":      settings.effort,
		"root":        ctx.root,
		"runtime":     runtime,
		"home":        home,
		"output":      outputFile,
		"prompt_file": promptFile,
		"instruction": ctx.instruction,
	}
	for name, path := range ctx.promptFilePaths {
		values["prompt_file:"+name] = path
	}
	arguments, err := process.ExpandArgvOmitEmpty(settings.reviewArgs, values, config.ReviewArgvPlaceholders(settings.promptFiles))
	if err != nil {
		return process.ProcessInvocation{}, "", newGateMsg(2, err.Error())
	}
	for key, tmpl := range settings.env {
		expanded, expErr := process.ExpandTemplate(tmpl, values, config.ReviewEnvPlaceholders())
		if expErr != nil {
			return process.ProcessInvocation{}, "", newGateMsg(2, expErr.Error())
		}
		environment[key] = expanded
	}
	cwd := runtime
	if settings.cwd == config.ReviewCWDRoot {
		cwd = ctx.root
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
	data, err := fs.ReadRegularFile(runtime, outputFile)
	if err != nil || len(data) == 0 || ctx.archive != nil && !utf8.Valid(data) {
		printFile(runtime, stdoutFile, os.Stdout)
		if ctx.archive != nil {
			printFile(runtime, outputFile, os.Stdout)
		}
		return incompleteReviewError(ctx.settings)
	}
	text, err := process.ParseReviewOutput(ctx.settings.output, string(data))
	if err != nil || strings.TrimSpace(text) == "" {
		printFile(runtime, outputFile, os.Stdout)
		return incompleteReviewError(ctx.settings)
	}
	if ctx.archive != nil {
		ctx.archive.report = []byte(text)
		_, _ = os.Stdout.Write(ctx.archive.report)
		syncStream(os.Stdout)
		return nil
	}
	fmt.Println(text)
	return nil
}

func incompleteReviewError(settings agentSettings) *gateError {
	return newGate(1, "review.review_did_not_complete_with_review_text", settings.name)
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
