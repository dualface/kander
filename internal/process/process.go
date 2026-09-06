// Package process resolves agent CLI entry points, writes UTF-8 task files, and builds executable process invocations.
//
// Task files get no POSIX permission or Windows ACL check; the agent is asked to delete one when it finishes, and a failed
// deletion or a leftover file does not affect the result. Windows prefers the native .exe; when only .cmd/.bat exists it uses
// an explicit cmd.exe /d /s /v:off /c with the encoded arguments carried in environment variables, never shell concatenation.
package process

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/i18n"
)

const (
	defaultTaskPrefix = "kander-task-"
	cmdEnvPrefix      = "KANDER_CMD_"
)

var windowsBatchSuffixes = map[string]struct{}{
	".cmd": {},
	".bat": {},
}

// isWindows can be injected in tests, matching the _is_windows mock of onevoke.
var isWindows = func() bool { return runtime.GOOS == "windows" }

// ErrNoCmd means no absolute path to cmd.exe could be resolved.
var ErrNoCmd = errors.New("could not resolve an absolute Windows cmd.exe path")

// ErrNUL means a Windows process argument contains NUL.
var ErrNUL = errors.New("Windows process arguments cannot contain NUL")

// AgentProgram is a resolved agent process entry point.
type AgentProgram struct {
	Path  string
	Batch bool
}

// ProcessInvocation is the argv and environment ready to hand to the process API.
type ProcessInvocation struct {
	Argv []string
	Env  map[string]string
	// ShellEnv is a subset of Env: only the variables this invocation minted,
	// holding the encoded argv values (the references live in Argv's /c text).
	// A terminal container (herdr pane, tmux window) receives a single shell
	// command rather than argv+env, so these variables must be assigned back
	// there first; that is what keeps the "the /c text contains nothing but
	// references this package generated" injection barrier intact.
	// nil off Windows and for invocations that are spawned directly.
	ShellEnv map[string]string
}

// ResolveAgentProgram resolves the process entry point of the four current agents per platform.
// name is the executable name, such as codex / claude / grok / cursor-agent.
func ResolveAgentProgram(name string) *AgentProgram {
	if !isWindows() {
		found, err := exec.LookPath(name)
		if err != nil {
			return nil
		}
		return &AgentProgram{Path: found}
	}

	if explicit := explicitProgram(name); explicit != nil {
		return explicit
	}
	if filepath.Ext(name) != "" {
		return nil
	}

	for _, candidate := range windowsPathCandidates(name, ".exe") {
		if usableFile(candidate, true) {
			return &AgentProgram{Path: candidate}
		}
	}
	for _, suffix := range []string{".cmd", ".bat"} {
		for _, candidate := range windowsPathCandidates(name, suffix) {
			if usableFile(candidate, false) {
				return &AgentProgram{Path: candidate, Batch: true}
			}
		}
	}
	return nil
}

func explicitProgram(name string) *AgentProgram {
	suffix := strings.ToLower(filepath.Ext(name))
	if suffix != ".exe" {
		if _, ok := windowsBatchSuffixes[suffix]; !ok {
			return nil
		}
	}
	if !usableFile(name, suffix == ".exe") {
		return nil
	}
	_, batch := windowsBatchSuffixes[suffix]
	return &AgentProgram{Path: name, Batch: batch}
}

func usableFile(path string, nonempty bool) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if nonempty && info.Size() == 0 {
		return false
	}
	return true
}

func windowsPathCandidates(name, suffix string) []string {
	filename := name
	if !strings.HasSuffix(strings.ToLower(name), suffix) {
		filename = name + suffix
	}
	separator := string(os.PathListSeparator)
	if isWindows() {
		separator = ";"
	}
	var out []string
	for _, raw := range strings.Split(os.Getenv("PATH"), separator) {
		directory := strings.Trim(strings.TrimSpace(raw), `"`)
		if directory == "" {
			continue
		}
		out = append(out, filepath.Join(directory, filename))
	}
	return out
}

func quoteWindowsBatchArgument(argument string) (string, error) {
	if strings.ContainsRune(argument, 0) {
		return "", ErrNUL
	}
	runes := []rune(argument)
	reverse := []rune{'"'}
	quoteHit := true
	for i := len(runes) - 1; i >= 0; i-- {
		character := runes[i]
		reverse = append(reverse, character)
		if quoteHit && character == '\\' {
			reverse = append(reverse, '\\')
		} else if character == '"' {
			quoteHit = true
			reverse = append(reverse, '"')
		} else {
			quoteHit = false
		}
	}
	reverse = append(reverse, '"')
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}
	return string(reverse), nil
}

func windowsPathName(p string) string {
	normalized := strings.ReplaceAll(p, `/`, `\`)
	if i := strings.LastIndex(normalized, `\`); i >= 0 {
		return normalized[i+1:]
	}
	return normalized
}

func windowsPathAbsolute(p string) bool {
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`) {
		return len(p) > 2
	}
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') && unicode.IsLetter(rune(p[0])) {
		return true
	}
	return false
}

func windowsJoin(base, elem1, elem2 string) string {
	base = strings.TrimRight(strings.ReplaceAll(base, `/`, `\`), `\`)
	return base + `\` + elem1 + `\` + elem2
}

// lookupEnv reads a value case-insensitively, the way Windows does:
// os.Environ keeps whatever spelling the parent process handed down, and
// containers differ (a herdr pane gives ComSpec and SYSTEMROOT), so looking the
// map up under one fixed spelling misses them.
func lookupEnv(environment map[string]string, name string) string {
	if value, ok := environment[name]; ok {
		return value
	}
	for key, value := range environment {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func windowsCommandInterpreter(environment map[string]string) (string, error) {
	comspec := lookupEnv(environment, "COMSPEC")
	if comspec != "" && windowsPathAbsolute(comspec) && strings.EqualFold(windowsPathName(comspec), "cmd.exe") {
		return comspec, nil
	}
	systemRoot := lookupEnv(environment, "SystemRoot")
	if systemRoot != "" && windowsPathAbsolute(systemRoot) {
		return windowsJoin(systemRoot, "System32", "cmd.exe"), nil
	}
	return "", ErrNoCmd
}

func cloneEnv(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func osEnvironMap() map[string]string {
	env := os.Environ()
	out := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			out[kv] = ""
			continue
		}
		out[k] = v
	}
	return out
}

func randomCmdNamespace() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return cmdEnvPrefix + strings.ToUpper(hex.EncodeToString(buf[:])), nil
}

// NewProcessInvocation builds the process invocation; the /c text of a batch call only contains variable references generated by this package.
func NewProcessInvocation(program AgentProgram, arguments []string, environment map[string]string) (ProcessInvocation, error) {
	return newInvocation(program, arguments, environment, false)
}

// NewShellInvocation builds an invocation that another shell will run as a
// single line (herdr pane, tmux window). On Windows it always takes cmd.exe's
// %VAR% form, batch or not: when PowerShell hands arguments to a native exe it
// rebuilds the command line, swallowing embedded double quotes, dropping empty
// arguments and letting a trailing backslash escape the closing quote. Only the
// variable-reference form restores argv byte for byte.
// On POSIX it is equivalent to NewProcessInvocation.
func NewShellInvocation(program AgentProgram, arguments []string, environment map[string]string) (ProcessInvocation, error) {
	return newInvocation(program, arguments, environment, true)
}

func newInvocation(program AgentProgram, arguments []string, environment map[string]string, forShell bool) (ProcessInvocation, error) {
	var child map[string]string
	if environment == nil {
		child = osEnvironMap()
	} else {
		child = cloneEnv(environment)
	}
	values := append([]string{program.Path}, arguments...)
	if !isWindows() || !(program.Batch || forShell) {
		return ProcessInvocation{Argv: values, Env: child}, nil
	}

	namespace, err := randomCmdNamespace()
	if err != nil {
		return ProcessInvocation{}, err
	}
	references := make([]string, 0, len(values))
	shell := make(map[string]string, len(values))
	for index, value := range values {
		variable := fmt.Sprintf("%s_%d", namespace, index)
		encoded, err := quoteWindowsBatchArgument(value)
		if err != nil {
			return ProcessInvocation{}, err
		}
		child[variable] = encoded
		shell[variable] = encoded
		references = append(references, "%"+variable+"%")
	}
	interpreter, err := windowsCommandInterpreter(child)
	if err != nil {
		return ProcessInvocation{}, err
	}
	return ProcessInvocation{
		Argv:     []string{interpreter, "/d", "/s", "/v:off", "/c", strings.Join(references, " ")},
		Env:      child,
		ShellEnv: shell,
	}, nil
}

func taskPayload(body, path string) string {
	return strings.TrimRightFunc(body, unicode.IsSpace) + "\n\n" +
		i18n.Text("cn", "process.task_cleanup", path)
}

// CreateTaskFile writes the task payload through the system temporary file mechanism, without additional permission or ACL checks.
func CreateTaskFile(body, prefix string) (string, error) {
	if prefix == "" {
		prefix = defaultTaskPrefix
	}
	handle, err := os.CreateTemp("", prefix+"*.md")
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(handle.Name())
	if err != nil {
		handle.Close()
		os.Remove(handle.Name())
		return "", err
	}
	if _, err := io.WriteString(handle, taskPayload(body, path)); err != nil {
		handle.Close()
		os.Remove(handle.Name())
		return "", err
	}
	if err := handle.Close(); err != nil {
		os.Remove(handle.Name())
		return "", err
	}
	return path, nil
}

// WriteTaskFile safely writes a new task payload relative to an existing runtime root.
func WriteTaskFile(root, path, body string) error {
	return fs.WriteTextAtomic(root, path, taskPayload(body, path), false)
}

// TaskFileInstruction builds the one-line file instruction handed to the agent CLI.
func TaskFileInstruction(prefix, path string) string {
	cleaned := strings.TrimRight(prefix, " .")
	return i18n.Text("en", "process.task_instruction", cleaned, path)
}
