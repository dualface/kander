package launch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/dualface/kander/internal/process"
)

type codexJSONEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
}

func launchStructuredAgent(plan LaunchPlan, invocation process.ProcessInvocation, cwd string, paneSession func() (AgentSession, error), agentSession *AgentSession) (LaunchOutcome, error) {
	cmd := exec.Command(invocation.Argv[0], invocation.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(invocation.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	if _, err := io.WriteString(stdin, plan.Prompt); err != nil {
		_ = cmd.Process.Kill()
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Process.Kill()
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}

	var mu sync.Mutex
	var code *int
	wait := make(chan int, 1)
	sessionReady := make(chan string, 1)
	go func() {
		defer close(sessionReady)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(os.Stdout, line)
			var event codexJSONEvent
			if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" && strings.TrimSpace(event.ThreadID) != "" {
				select {
				case sessionReady <- event.ThreadID:
				default:
				}
			}
		}
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else {
				exitCode = 1
			}
		}
		mu.Lock()
		code = &exitCode
		mu.Unlock()
		wait <- exitCode
		close(wait)
	}()

	outcome := LaunchOutcome{Process: cmd.Process}
	outcome.Wait = func() (int, error) { return <-wait, nil }
	outcome.Poll = func() *int {
		mu.Lock()
		defer mu.Unlock()
		if code == nil {
			return nil
		}
		value := *code
		return &value
	}
	if paneSession != nil {
		session, err := paneSession()
		if err != nil {
			_ = cmd.Process.Kill()
			return LaunchOutcome{}, err
		}
		if session.Reference == "" {
			reference, ok := <-sessionReady
			if !ok || reference == "" {
				_ = cmd.Process.Kill()
				return LaunchOutcome{}, launchError("launch.agent_allocate_invalid")
			}
			session.Reference = reference
		}
		outcome.Session = session.Reference
		if agentSession != nil {
			*agentSession = session
		}
		if plan.sessionFinalize != nil {
			if err := plan.sessionFinalize(session); err != nil {
				_ = cmd.Process.Kill()
				return LaunchOutcome{}, err
			}
		}
	}
	return outcome, nil
}
