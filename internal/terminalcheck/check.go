// Package terminalcheck runs observable conformance checks through Backend.
package terminalcheck

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// Options controls only destructive cleanup and optional focus. Each command
// and polling operation has a deadline; cleanup gets a fresh independent one.
type Options struct {
	Keep      bool
	SkipFocus bool
}

type checker struct {
	backend    terminal.Backend
	conn       terminal.Conn
	options    Options
	out        io.Writer
	cwd        string
	process    string
	nonce      string
	address    terminal.Address
	target     terminal.Target
	created    bool
	closed     bool
	writeErr   error
	lastStderr string
}

type step struct {
	method     string
	capability string
	check      func(*checker, bool) (string, error)
}

// methodSteps is also checked against the Backend interface by reflection.
// Checks with the same method (metadata/foreground and post-close facts) are
// supplemental assertions, rather than a second interface inventory. Optional
// capability expressions select success versus unsupported assertions in wait,
// metadata, report, facts, reverse and focus. Container is instead a prerequisite
// enforced by the Capabilities step: if absent, that step fails and later steps
// are not executed. Container checks therefore ignore their evaluated boolean;
// empty expressions impose no capability requirement. Metadata readback belongs
// to SetSessionMarker, not the PaneFacts step.
var methodSteps = []step{
	{"Name", "", (*checker).name},
	{"Capabilities", "", (*checker).capabilities},
	{"Executable", "", (*checker).executable},
	{"VersionArgs", "", (*checker).version},
	{"AutoDetect", "", (*checker).autoDetect},
	{"Prepare", "", (*checker).prepare},
	{"CreateContainer", "Container", (*checker).create},
	{"OpaqueAddress", "Container", (*checker).opaque},
	{"ParseAddress", "Container", (*checker).parse},
	{"ParseFocusAddress", "Container", (*checker).parseFocus},
	{"StartedLines", "Container", (*checker).started},
	{"WaitReady", "Container", (*checker).ready},
	{"RunCommand", "Container", (*checker).run},
	{"WaitOutput", "WaitOutput", (*checker).wait},
	{"ReadOutput", "Container", (*checker).read},
	{"SetSessionMarker", "PaneMetadata", (*checker).metadata},
	{"ReportSession", "SessionReport", (*checker).report},
	{"PaneFacts", "ForegroundProcess", (*checker).facts},
	{"Topology", "Container", (*checker).topology},
	{"ContainerExists", "Container", (*checker).exists},
	{"ReverseLookup", "PaneMetadata|AgentIdentity&SessionReport", (*checker).reverse},
	{"Focus", "Focus", (*checker).focus},
	{"DeliverText", "Container", (*checker).deliver},
	{"CloseContainer", "Container", (*checker).close},
}

// Check runs one prepared definition launcher. It stops at the first failure,
// reports unexecuted steps, and always attempts cleanup unless Keep is set.
func Check(backend terminal.Backend, options Options, out io.Writer) (result error) {
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	cwd, err := os.MkdirTemp("", "kander-terminal-test-")
	if err != nil {
		return err
	}
	c := &checker{backend: backend, options: options, out: out, cwd: cwd, nonce: hex.EncodeToString(token[:])}
	c.conn = terminal.Conn{Program: backend.Executable(), Run: c.trace}
	defer func() {
		defer func() {
			if !c.created || c.closed {
				result = errors.Join(result, os.RemoveAll(cwd))
			}
		}()
		if c.created && !c.closed {
			if options.Keep {
				c.print(config.Text("terminal.test_kept", terminal.FormatAddress(backend, c.address), c.cwd))
			} else {
				_, err := c.closeContainer()
				c.status("cleanup", "", err)
				result = errors.Join(result, err)
				if err != nil {
					c.print(config.Text("terminal.test_kept", terminal.FormatAddress(backend, c.address), c.cwd))
				}
			}
		}
		result = errors.Join(result, c.writeErr)
	}()
	for _, s := range methodSteps {
		if result != nil {
			c.status(s.method, config.Text("terminal.test_after_failure"), nil)
			continue
		}
		note, err := s.execute(c)
		c.status(s.method, note, err)
		if err != nil {
			result = fmt.Errorf("%s: %w", s.method, err)
		}
		if c.writeErr != nil {
			result = errors.Join(result, c.writeErr)
		}
	}
	if result != nil {
		c.status("PaneFacts.gone", config.Text("terminal.test_after_failure"), nil)
	} else if options.Keep {
		c.status("PaneFacts.gone", config.Text("terminal.test_keep_skip"), nil)
	} else {
		_, err := c.gone()
		c.status("PaneFacts.gone", "", err)
		result = err
	}
	return result
}

func (c *checker) print(line string) {
	if c.writeErr != nil {
		return
	}
	_, c.writeErr = fmt.Fprintln(c.out, line)
}

func (c *checker) status(name, skip string, err error) {
	state, detail := "pass", ""
	if skip != "" {
		state, detail = "skip", skip
	}
	if err != nil {
		state, detail = "fail", err.Error()
	}
	c.print(config.Text("terminal.test_step", state, name, detail))
}

func (c *checker) trace(ctx context.Context, program string, args []string) (probe.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := terminal.ProbeRunner(ctx, program, args)
	argv, _ := json.Marshal(append([]string{program}, args...))
	c.print(config.Text("terminal.test_trace", string(argv), res.Code, summary(res.Stdout), summary(res.Stderr), fmt.Sprint(err)))
	c.lastStderr = strings.TrimSpace(res.Stderr)
	return res, err
}

func summary(text string) string {
	const limit = 1024
	runes := []rune(text)
	if len(runes) > limit {
		text = string(runes[:limit]) + "…"
	}
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

func (c *checker) mismatch(want, got any) error {
	return fmt.Errorf("%s", config.Text("terminal.test_mismatch", fmt.Sprint(want), fmt.Sprint(got)))
}

func (c *checker) unsupported(err error) (string, error) {
	if !errors.Is(err, terminal.ErrUnsupported) {
		return "", c.mismatch(terminal.ErrUnsupported, err)
	}
	return config.Text("terminal.test_unsupported"), nil
}

func (c *checker) name(_ bool) (string, error) {
	if c.backend.Name() == "" {
		return "", c.mismatch("name", "")
	}
	c.print(c.backend.Name())
	return "", nil
}
func (c *checker) capabilities(_ bool) (string, error) {
	data, _ := json.Marshal(c.backend.Capabilities())
	c.print(string(data))
	if !c.backend.Capabilities().Container {
		return "", fmt.Errorf("%s", config.Text("terminal.test_unavailable"))
	}
	return "", nil
}
func (c *checker) executable(_ bool) (string, error) {
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("%s", config.Text("terminal.test_unavailable"))
	}
	path, err := exec.LookPath(c.backend.Executable())
	if err != nil {
		return "", err
	}
	c.conn.Program = path
	return "", nil
}
func (c *checker) version(_ bool) (string, error) {
	args := c.backend.VersionArgs()
	if len(args) == 0 {
		return config.Text("terminal.test_no_version"), nil
	}
	res, err := c.conn.Run(context.Background(), c.conn.Program, args)
	if err == nil && (res.Code != 0 || strings.TrimSpace(res.Stdout+res.Stderr) == "") {
		err = c.mismatch("version, exit 0", res)
	}
	return "", err
}
func (c *checker) autoDetect(_ bool) (string, error) {
	c.print(fmt.Sprintf("auto=%t", c.backend.AutoDetect(os.Getenv)))
	return "", nil
}
func (c *checker) prepare(_ bool) (string, error) {
	request := terminal.PrepareRequest{Project: c.cwd, Command: "sh", Windows: runtime.GOOS == "windows", LookPath: exec.LookPath, Getenv: os.Getenv, TTY: func() bool { return true }}
	var err error
	if backend, ok := c.backend.(*terminal.DeclarativeBackend); ok {
		c.target, err = backend.PrepareWithRunner(request, c.trace)
	} else {
		c.target, err = c.backend.Prepare(request)
	}
	if err == nil {
		c.conn.Program = c.target.Program
	}
	return "", err
}
func (c *checker) create(_ bool) (string, error) {
	var err error
	c.address, err = c.backend.CreateContainer(c.conn, c.target, c.cwd, "kander-test-"+c.nonce)
	c.created = c.address.Container != ""
	if err == nil && (c.address.Container == "" || c.address.Pane == "") {
		err = c.mismatch("container/pane", c.address)
	}
	return "", err
}
func (c *checker) opaque(_ bool) (string, error) {
	if c.backend.OpaqueAddress(c.address) == "" {
		return "", c.mismatch("address", "")
	}
	return "", nil
}
func (c *checker) parse(_ bool) (string, error) {
	address, ok := c.backend.ParseAddress(terminal.FormatAddress(c.backend, c.address))
	if !ok || address != c.address {
		return "", c.mismatch(c.address, address)
	}
	if _, ok := c.backend.ParseAddress("invalid\naddress"); ok {
		return "", c.mismatch(false, true)
	}
	return "", nil
}
func (c *checker) parseFocus(_ bool) (string, error) {
	address, ok := c.backend.ParseFocusAddress(strings.Split(terminal.FormatAddress(c.backend, c.address), ":"))
	if !ok || address != c.address {
		return "", c.mismatch(c.address, address)
	}
	return "", nil
}
func (c *checker) started(_ bool) (string, error) {
	for _, line := range c.backend.StartedLines("kander-test", c.target, c.address, os.Getenv) {
		c.print(line)
	}
	return "", nil
}
func (c *checker) ready(_ bool) (string, error) {
	return "", c.backend.WaitReady(c.conn, c.address.Pane)
}

// The marker is assembled inside the pane so a shell echo of RunCommand
// cannot satisfy the output check. The shell stays alive to acknowledge input.
func (c *checker) run(_ bool) (string, error) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		return "", err
	}
	script := `process=$(ps -p "$$" -o comm=) || exit; process=${process##*/}; printf '%s%s:%s\n' 'kander-process-' '` + c.nonce + `' "$process"; printf '%s%s\n' 'kander-ready-' '` + c.nonce + `'; while IFS= read -r line; do printf '%s%s\n' 'kander-ack-' "$line"; done`
	command := quote(shell) + " -c " + quote(script)
	return "", c.backend.RunCommand(c.conn, c.address.Pane, command, true)
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }
func (c *checker) wait(supported bool) (string, error) {
	err := c.backend.WaitOutput(context.Background(), c.conn, c.address.Pane, "kander-ready-"+c.nonce, 5000)
	if !supported {
		return c.unsupported(err)
	}
	return "", err
}
func (c *checker) read(_ bool) (string, error) {
	err := c.poll(func(ctx context.Context) (bool, error) {
		output, err := c.backend.ReadOutput(ctx, c.conn, c.address.Pane)
		if err != nil {
			return false, err
		}
		if !strings.Contains(output, "kander-ready-"+c.nonce) {
			return false, nil
		}
		marker := "kander-process-" + c.nonce + ":"
		for _, line := range strings.Split(output, "\n") {
			if process, ok := strings.CutPrefix(strings.TrimSpace(line), marker); ok {
				c.process = strings.TrimSpace(process)
				if c.process != "" {
					return true, nil
				}
			}
		}
		return false, c.mismatch("shell process marker", output)
	})
	return "", err
}
func (c *checker) awaitOutput(marker string) error {
	return c.poll(func(ctx context.Context) (bool, error) {
		output, err := c.backend.ReadOutput(ctx, c.conn, c.address.Pane)
		if err != nil {
			return false, err
		}
		return strings.Contains(output, marker), nil
	})
}
func (c *checker) poll(check func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (c *checker) metadata(supported bool) (string, error) {
	err := c.backend.SetSessionMarker(c.conn, c.address.Pane, c.nonce)
	if !supported {
		note, err := c.unsupported(err)
		if err != nil {
			return "", err
		}
		facts, err := c.backend.PaneFacts(context.Background(), c.conn, c.address.Pane)
		if err == nil && (facts.Gone || facts.SessionMarker != "") {
			err = c.mismatch("no session marker on a live pane", facts)
		}
		return note, err
	}
	if err != nil {
		return "", err
	}
	facts, err := c.backend.PaneFacts(context.Background(), c.conn, c.address.Pane)
	if err == nil && (facts.Gone || facts.SessionMarker != c.nonce) {
		err = c.mismatch(c.nonce, facts.SessionMarker)
	}
	return "", err
}
func (c *checker) report(supported bool) (string, error) {
	err := c.backend.ReportSession(terminal.SessionReport{Conn: c.conn, Pane: c.address.Pane, Agent: "codex", Reference: c.nonce, Deadline: time.Now().Add(5 * time.Second), Now: time.Now})
	if !supported {
		return c.unsupported(err)
	}
	if err != nil {
		return "", err
	}
	facts, err := c.backend.PaneFacts(context.Background(), c.conn, c.address.Pane)
	if err == nil && (facts.Gone || facts.AgentSession != c.nonce) {
		err = c.mismatch(c.nonce, facts)
	}
	return "", err
}
func (c *checker) facts(supported bool) (string, error) {
	if !supported {
		facts, err := c.backend.PaneFacts(context.Background(), c.conn, c.address.Pane)
		if err == nil && (facts.Gone || facts.Command != "" || facts.Dead != "" || facts.InMode != "") {
			err = c.mismatch("no foreground fields on a live pane", facts)
		}
		return config.Text("terminal.test_no_foreground"), err
	}
	err := c.poll(func(ctx context.Context) (bool, error) {
		facts, err := c.backend.PaneFacts(ctx, c.conn, c.address.Pane)
		if err != nil {
			return false, err
		}
		if facts.Gone {
			return false, c.mismatch("live pane", facts)
		}
		return facts.Command == c.process && facts.Dead == "0" && facts.InMode == "0", nil
	})
	return "", err
}
func (c *checker) topology(_ bool) (string, error) {
	topology, err := c.backend.Topology(context.Background(), c.conn, c.address)
	if err == nil && (topology.Container != c.address.Container || (topology.PaneCount != "1" && !(len(topology.Panes) == 1 && topology.Panes[0] == c.address.Pane))) {
		err = c.mismatch(c.address, topology)
	}
	return "", err
}
func (c *checker) exists(_ bool) (string, error) {
	exists, err := c.backend.ContainerExists(context.Background(), c.conn, c.address)
	if err == nil && !exists {
		err = c.mismatch(true, exists)
	}
	return "", err
}
func (c *checker) reverse(supported bool) (string, error) {
	address, err := c.backend.ReverseLookup(context.Background(), c.conn, terminal.Identity{Agent: "codex", Reference: c.nonce, ProcessName: func() (string, error) { return c.process, nil }})
	if !supported {
		// Without a writable identity the lookup must complete with zero matches.
		var match *terminal.MatchError
		if !errors.As(err, &match) || match.Matches != 0 {
			return "", c.mismatch("zero matches", err)
		}
		return config.Text("terminal.test_no_identity"), nil
	}
	if err == nil && address != c.address {
		err = c.mismatch(c.address, address)
	}
	return "", err
}
func (c *checker) focus(supported bool) (string, error) {
	if c.options.SkipFocus && supported {
		return config.Text("terminal.test_focus_skip"), nil
	}
	c.lastStderr = ""
	result := c.backend.Focus(context.Background(), c.conn, c.address)
	if !supported {
		if result.Success || result.ID != "focus.unsupported" {
			return "", c.mismatch("unsupported focus", result)
		}
		return config.Text("terminal.test_unsupported"), nil
	}
	if result.Success {
		return "", nil
	}
	// FocusResult has no error kind. Only the backend's explicit outside-tmux
	// result and tmux's exact no-client diagnostic establish this environment skip.
	if result.ID == "focus.outside_tmux" || c.lastStderr == "no current client" || c.lastStderr == "no clients" {
		return config.Text("terminal.test_no_client"), nil
	}
	return "", fmt.Errorf("%s", config.Text(result.ID, result.Args...))
}
func (c *checker) deliver(_ bool) (string, error) {
	text := "literal-" + c.nonce + ` $HOME; ' " \\`
	if err := c.backend.DeliverText(context.Background(), c.conn, c.address.Pane, text); err != nil {
		return "", err
	}
	return "", c.awaitOutput("kander-ack-" + text)
}
func (c *checker) close(_ bool) (string, error) {
	if c.options.Keep {
		return config.Text("terminal.test_keep_skip"), nil
	}
	return c.closeContainer()
}
func (c *checker) closeContainer() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.backend.CloseContainer(ctx, c.conn, c.address)
	if err == nil {
		c.closed = true
	}
	return "", err
}
func (c *checker) gone() (string, error) {
	facts, err := c.backend.PaneFacts(context.Background(), c.conn, c.address.Pane)
	if err == nil && !facts.Gone {
		err = c.mismatch("gone", facts)
	}
	exists, existsErr := c.backend.ContainerExists(context.Background(), c.conn, c.address)
	if existsErr == nil && exists {
		existsErr = c.mismatch(false, exists)
	}
	return "", errors.Join(err, existsErr)
}
