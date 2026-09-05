package launch

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
)

func isWindowsGOOS() bool { return runtime.GOOS == "windows" }

func lookPathExec(name string) (string, error) {
	return exec.LookPath(name)
}

func fileIsTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

type startedProc struct {
	proc *os.Process
	mu   sync.Mutex
	code *int
	wait chan int
}

func (s *startedProc) Wait() (int, error) {
	c, ok := <-s.wait
	if !ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.code != nil {
			return *s.code, nil
		}
		return 0, nil
	}
	return c, nil
}

func (s *startedProc) Poll() *int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == nil {
		return nil
	}
	v := *s.code
	return &v
}

func startProcess(argv []string, env map[string]string, cwd string, console bool) (*startedProc, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(env)
	if console {
		applyConsoleAttr(cmd)
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	wait := make(chan int, 1)
	handle := &startedProc{proc: cmd.Process, wait: wait}
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		handle.mu.Lock()
		handle.code = &code
		handle.mu.Unlock()
		wait <- code
		close(wait)
	}()
	return handle, nil
}

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}

func killProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
