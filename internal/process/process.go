// Package process 解析 Agent CLI 入口, 写入 UTF-8 任务文件, 并构造可 exec 的进程调用.
//
// 任务文件不检查 POSIX 权限或 Windows ACL; 要求 Agent 完成后尝试删除,
// 删除失败或遗留不影响结果. Windows 优先原生 .exe; 只有 .cmd/.bat 时
// 用显式 cmd.exe /d /s /v:off /c 和环境变量承载已编码参数, 不用 shell 拼接.
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

// isWindows 可在测试中注入, 对标 onevoke 的 _is_windows mock.
var isWindows = func() bool { return runtime.GOOS == "windows" }

// ErrNoCmd 表示无法解析绝对路径的 cmd.exe.
var ErrNoCmd = errors.New("could not resolve an absolute Windows cmd.exe path")

// ErrNUL 表示 Windows 进程参数含 NUL.
var ErrNUL = errors.New("Windows process arguments cannot contain NUL")

// AgentProgram 是已解析的 Agent 进程入口.
type AgentProgram struct {
	Path  string
	Batch bool
}

// ProcessInvocation 是可直接交给进程 API 的 argv 与环境.
type ProcessInvocation struct {
	Argv []string
	Env  map[string]string
}

// ResolveAgentProgram 按平台解析当前四种 Agent 的进程入口.
// name 是可执行文件名, 如 codex / claude / grok / cursor-agent.
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

func windowsCommandInterpreter(environment map[string]string) (string, error) {
	comspec := environment["COMSPEC"]
	if comspec != "" && windowsPathAbsolute(comspec) && strings.EqualFold(windowsPathName(comspec), "cmd.exe") {
		return comspec, nil
	}
	systemRoot := environment["SystemRoot"]
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

// NewProcessInvocation 构造进程调用; batch 的 /c 文本只含本包生成的变量引用.
func NewProcessInvocation(program AgentProgram, arguments []string, environment map[string]string) (ProcessInvocation, error) {
	var child map[string]string
	if environment == nil {
		child = osEnvironMap()
	} else {
		child = cloneEnv(environment)
	}
	values := append([]string{program.Path}, arguments...)
	if !isWindows() || !program.Batch {
		return ProcessInvocation{Argv: values, Env: child}, nil
	}

	namespace, err := randomCmdNamespace()
	if err != nil {
		return ProcessInvocation{}, err
	}
	references := make([]string, 0, len(values))
	for index, value := range values {
		variable := fmt.Sprintf("%s_%d", namespace, index)
		encoded, err := quoteWindowsBatchArgument(value)
		if err != nil {
			return ProcessInvocation{}, err
		}
		child[variable] = encoded
		references = append(references, "%"+variable+"%")
	}
	interpreter, err := windowsCommandInterpreter(child)
	if err != nil {
		return ProcessInvocation{}, err
	}
	return ProcessInvocation{
		Argv: []string{interpreter, "/d", "/s", "/v:off", "/c", strings.Join(references, " ")},
		Env:  child,
	}, nil
}

func taskPayload(body, path string) string {
	return strings.TrimRightFunc(body, unicode.IsSpace) + "\n\n" +
		i18n.Text("cn", "process.task_cleanup", path)
}

// CreateTaskFile 用系统临时文件机制写入任务载荷, 不附加权限或 ACL 检查.
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

// WriteTaskFile 相对已有 runtime 根安全写入新任务载荷.
func WriteTaskFile(root, path, body string) error {
	return fs.WriteTextAtomic(root, path, taskPayload(body, path), false)
}

// TaskFileInstruction 生成交给 Agent CLI 的一句短文件指令.
func TaskFileInstruction(prefix, path string) string {
	cleaned := strings.TrimRight(prefix, " .")
	return i18n.Text("en", "process.task_instruction", cleaned, path)
}
