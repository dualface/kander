package launch

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"

	"github.com/dualface/kander/v2/internal/process"
)

// cursorSessionProvider creates the chat that becomes the session with
// `cursor-agent create-chat` before the launch.
type cursorSessionProvider struct{}

func init() { registerSessionProvider(cursorSessionProvider{}) }

func (cursorSessionProvider) Mode() string    { return "hook:cursor-create-chat" }
func (cursorSessionProvider) Resumable() bool { return true }

func (cursorSessionProvider) Allocate(l sessionLaunch) (string, error) {
	return cursorCreateChat(l.Program)
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
