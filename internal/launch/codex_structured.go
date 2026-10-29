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
	"time"

	"github.com/dualface/kander/internal/process"
)

const (
	structuredCleanupGrace      = 500 * time.Millisecond
	structuredMaxJSONLRecordLen = 8 * 1024 * 1024
)

type codexJSONEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

type structuredSessionResult struct {
	reference string
	err       error
}

type structuredProcessResult struct {
	code     int
	err      error
	response string
}

type structuredProcess struct {
	cmd         *exec.Cmd
	done        chan struct{}
	stopSignals func()
	mu          sync.Mutex
	result      structuredProcessResult
}

func (p *structuredProcess) finish(result structuredProcessResult) {
	p.mu.Lock()
	p.result = result
	p.mu.Unlock()
	close(p.done)
}

func (p *structuredProcess) Wait() (int, error) {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result.code, p.result.err
}

func (p *structuredProcess) Poll() *int {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		code := p.result.code
		return &code
	default:
		return nil
	}
}

func (p *structuredProcess) Response() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result.response
}

func launchStructuredAgent(plan LaunchPlan, invocation process.ProcessInvocation, cwd string, paneSession func() (AgentSession, error), agentSession *AgentSession, durable ...bool) (LaunchOutcome, error) {
	cmd := exec.Command(invocation.Argv[0], invocation.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(invocation.Env)
	cmd.Stderr = os.Stderr
	configureStructuredProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	if err := cmd.Start(); err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}

	proc := &structuredProcess{cmd: cmd, done: make(chan struct{})}
	if err := attachStructuredProcess(cmd); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	proc.stopSignals = watchStructuredSignals(cmd, proc.done)
	sessionReady := make(chan structuredSessionResult, 1)
	go drainStructuredOutput(proc, stdout, sessionReady)
	outcome := LaunchOutcome{Process: cmd.Process, Wait: proc.Wait, Poll: proc.Poll, cleanup: func() { cleanupStructuredProcess(proc) }, response: proc.Response}
	delivered := false
	isDurable := len(durable) > 0 && durable[0]
	fail := func(cause error) (LaunchOutcome, error) {
		cleanupStructuredProcess(proc)
		failure := &LaunchFailure{Err: cause}
		if delivered && isDurable {
			failure.DeliveryUnknown = true
		}
		return LaunchOutcome{}, failure
	}

	written, err := io.WriteString(stdin, plan.Prompt)
	delivered = written > 0
	if err != nil {
		_ = stdin.Close()
		return fail(launchError("launch.failed_to_start_agent", err.Error()))
	}
	delivered = true
	if err := stdin.Close(); err != nil {
		return fail(launchError("launch.failed_to_start_agent", err.Error()))
	}

	if paneSession == nil {
		return outcome, nil
	}
	session, err := paneSession()
	if err != nil {
		return fail(err)
	}
	if session.Reference == "" {
		timer := time.NewTimer(plan.launchTimeout(sessionDiscoverWait))
		defer timer.Stop()
		select {
		case found := <-sessionReady:
			if found.err != nil {
				return fail(found.err)
			}
			session.Reference = found.reference
		case <-proc.done:
			select {
			case found := <-sessionReady:
				if found.err != nil {
					return fail(found.err)
				}
				session.Reference = found.reference
			default:
				code, waitErr := proc.Wait()
				if waitErr != nil {
					return fail(waitErr)
				}
				return fail(launchError("launch.failed_to_start_agent", fmt.Sprintf("exit %d before thread.started", code)))
			}
		case <-timer.C:
			return fail(launchError("launch.the_newly_started_session_has_not_appeared_yet"))
		}
	}
	if !validSessionReference(session.Reference) {
		return fail(launchError("launch.agent_allocate_invalid"))
	}
	outcome.Session = session.Reference
	if agentSession != nil {
		*agentSession = session
	}
	if plan.sessionFinalize != nil {
		if err := plan.sessionFinalize(session); err != nil {
			return fail(err)
		}
	}
	return outcome, nil
}

func drainStructuredOutput(proc *structuredProcess, stdout io.Reader, sessionReady chan<- structuredSessionResult) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), structuredMaxJSONLRecordLen)
	var parseErr error
	var response string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var event codexJSONEvent
		if err := json.Unmarshal(line, &event); err != nil {
			if parseErr == nil {
				parseErr = fmt.Errorf("Codex JSONL parse error: %w", err)
				select {
				case sessionReady <- structuredSessionResult{err: parseErr}:
				default:
				}
			}
		} else if event.Type == "thread.started" {
			ref := strings.TrimSpace(event.ThreadID)
			result := structuredSessionResult{reference: ref}
			if !validSessionReference(ref) {
				result = structuredSessionResult{err: launchError("launch.agent_allocate_invalid")}
			}
			select {
			case sessionReady <- result:
			default:
			}
		} else if event.Type == "item.completed" && event.Item.Type == "agent_message" {
			response = event.Item.Text
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && parseErr == nil {
		if strings.Contains(scanErr.Error(), "token too long") {
			parseErr = fmt.Errorf("Codex JSONL record is too large: maximum %d bytes", structuredMaxJSONLRecordLen)
		} else {
			parseErr = fmt.Errorf("Codex JSONL read error: %w", scanErr)
		}
		select {
		case sessionReady <- structuredSessionResult{err: parseErr}:
		default:
		}
		_ = signalStructuredProcess(proc.cmd, true)
	}
	waitErr := proc.cmd.Wait()
	if proc.stopSignals != nil {
		proc.stopSignals()
	}
	releaseStructuredProcess(proc.cmd)
	code := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if parseErr == nil {
			parseErr = waitErr
		}
	}
	proc.finish(structuredProcessResult{code: code, err: parseErr, response: response})
}

func cleanupStructuredProcess(proc *structuredProcess) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return
	}
	select {
	case <-proc.done:
		return
	default:
	}
	_ = signalStructuredProcess(proc.cmd, false)
	select {
	case <-proc.done:
		return
	case <-time.After(structuredCleanupGrace):
	}
	_ = signalStructuredProcess(proc.cmd, true)
	select {
	case <-proc.done:
	case <-time.After(structuredCleanupGrace):
	}
}
