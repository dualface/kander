package launch

import (
	"bufio"
	"encoding/json"
	"errors"
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
	structuredPromptWriteWait   = time.Second
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

type structuredOutputResult struct {
	err      error
	response string
}

type structuredProcess struct {
	cmd             *exec.Cmd
	tree            *structuredProcessTree
	stdin           *os.File
	stdout          *os.File
	done            chan struct{}
	stopSignals     func()
	stdinCloseOnce  sync.Once
	stdoutCloseOnce sync.Once
	stdinCloseErr   error
	stdoutCloseErr  error
	mu              sync.Mutex
	result          structuredProcessResult
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

func (p *structuredProcess) closeStdin() error {
	p.stdinCloseOnce.Do(func() {
		if p.stdin != nil {
			p.stdinCloseErr = p.stdin.Close()
		}
	})
	return p.stdinCloseErr
}

func (p *structuredProcess) closeStdout() error {
	p.stdoutCloseOnce.Do(func() {
		if p.stdout != nil {
			p.stdoutCloseErr = p.stdout.Close()
		}
	})
	return p.stdoutCloseErr
}

func writeStructuredPrompt(writer io.Writer, closeWriter func() error, cancel func() error, prompt string, timeout time.Duration) (int, error) {
	type writeResult struct {
		written int
		err     error
	}
	written := make(chan writeResult, 1)
	go func() {
		count, err := io.WriteString(writer, prompt)
		written <- writeResult{written: count, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	finish := func(result writeResult) (int, error) {
		return result.written, errors.Join(result.err, closeWriter())
	}
	select {
	case result := <-written:
		return finish(result)
	case <-timer.C:
		// Prefer a completed write when it raced with the timer.
		select {
		case result := <-written:
			return finish(result)
		default:
		}
	}

	// The owned process tree is the only possible reader after startup. Closing
	// it first releases a full pipe. Closing the writer then cancels any I/O
	// still pending in the Go runtime; wait for the writer before returning so
	// its byte count remains authoritative for durable-delivery uncertainty.
	cancelErr := cancel()
	closeErr := closeWriter()
	result := <-written
	return result.written, errors.Join(
		fmt.Errorf("structured prompt write timed out after %s", timeout),
		result.err,
		cancelErr,
		closeErr,
	)
}

func launchStructuredAgent(plan LaunchPlan, invocation process.ProcessInvocation, cwd string, paneSession func() (AgentSession, error), agentSession *AgentSession, durable ...bool) (LaunchOutcome, error) {
	cmd := exec.Command(invocation.Argv[0], invocation.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(invocation.Env)
	tree, err := newStructuredProcessTree(cmd)
	if err != nil {
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		_ = tree.close()
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdinReader.Close()
		_ = stdinWriter.Close()
		_ = tree.close()
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	cmd.Stdin = stdinReader
	cmd.Stdout = stdoutWriter
	// Structured diagnostics belong to the process boundary. They must not
	// write into a CLI or TUI global stream.
	cmd.Stderr = nil
	if err := tree.start(); err != nil {
		_ = stdinReader.Close()
		_ = stdinWriter.Close()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		if cmd.Process != nil {
			_ = tree.signal(true)
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = tree.close()
		return LaunchOutcome{}, launchError("launch.failed_to_start_agent", err.Error())
	}
	_ = stdinReader.Close()
	_ = stdoutWriter.Close()

	proc := &structuredProcess{cmd: cmd, tree: tree, stdin: stdinWriter, stdout: stdoutReader, done: make(chan struct{})}
	proc.stopSignals = watchStructuredSignals(tree, proc.done)
	sessionReady := make(chan structuredSessionResult, 1)
	go reapStructuredProcess(proc, sessionReady)
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

	written, err := writeStructuredPrompt(
		stdinWriter,
		proc.closeStdin,
		func() error { return proc.tree.signal(true) },
		plan.Prompt,
		plan.launchTimeout(structuredPromptWriteWait),
	)
	delivered = written > 0
	if err != nil {
		return fail(launchError("launch.failed_to_start_agent", err.Error()))
	}
	delivered = true

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

func drainStructuredOutput(proc *structuredProcess, sessionReady chan<- structuredSessionResult) structuredOutputResult {
	scanner := bufio.NewScanner(proc.stdout)
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
		_ = proc.tree.signal(true)
	}
	return structuredOutputResult{err: parseErr, response: response}
}

func reapStructuredProcess(proc *structuredProcess, sessionReady chan<- structuredSessionResult) {
	outputDone := make(chan structuredOutputResult, 1)
	go func() { outputDone <- drainStructuredOutput(proc, sessionReady) }()
	waitErr := proc.cmd.Wait()
	if proc.stopSignals != nil {
		proc.stopSignals()
	}
	// The direct process is reaped, so no legitimate descendant remains. Kill
	// the owned tree before waiting for inherited output handles to close.
	_ = proc.tree.signal(true)
	treeCloseErr := proc.tree.close()
	var output structuredOutputResult
	select {
	case output = <-outputDone:
	case <-time.After(structuredCleanupGrace):
		_ = proc.closeStdout()
		select {
		case output = <-outputDone:
		case <-time.After(structuredCleanupGrace):
			output.err = errors.New("Codex JSONL output did not stop after the owned pipe was closed")
		}
	}
	if errors.Is(output.err, os.ErrClosed) {
		output.err = nil
	}
	_ = proc.closeStdout()
	code := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if output.err == nil {
			output.err = waitErr
		}
	}
	output.err = errors.Join(output.err, treeCloseErr)
	proc.finish(structuredProcessResult{code: code, err: output.err, response: output.response})
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
	_ = proc.closeStdin()
	_ = proc.tree.signal(false)
	select {
	case <-proc.done:
		return
	case <-time.After(structuredCleanupGrace):
	}
	_ = proc.closeStdout()
	_ = proc.tree.signal(true)
	select {
	case <-proc.done:
	case <-time.After(structuredCleanupGrace):
	}
}
