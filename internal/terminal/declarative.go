package terminal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

// DeclarativeBackend is one launcher of a validated terminal definition. It
// builds argv from the definition steps, runs, polls, parses and classifies
// them; no terminal-specific Go code is involved.
type DeclarativeBackend struct {
	def         *Definition
	launcher    string
	spec        LauncherDefinition
	getenv      func(string) string
	addressRe   *regexp.Regexp
	gone        []errorRule
	metaMissing []errorRule
}

var _ Backend = (*DeclarativeBackend)(nil)

// NewDeclarativeBackend returns the backend of one launcher the definition
// provides. getenv reads the environment of env.<NAME> references.
func NewDeclarativeBackend(def *Definition, launcher string, getenv func(string) string) (*DeclarativeBackend, error) {
	spec, ok := def.Launchers[launcher]
	if !ok {
		return nil, fmt.Errorf("terminal: definition %s does not provide launcher %s", def.Name, launcher)
	}
	backend := &DeclarativeBackend{def: def, launcher: launcher, spec: spec}
	backend.getenv = backend.launcherGetenv(getenv)
	parts := make([]string, len(def.Address))
	for index, field := range def.Address {
		pattern := field.Pattern
		if pattern == "" {
			pattern = `[^:\s]+`
		}
		parts[index] = "(" + pattern + ")"
	}
	backend.addressRe = regexp.MustCompile("^" + regexp.QuoteMeta(launcher) + ":" + strings.Join(parts, ":") + "$")
	for _, rule := range def.Errors.Gone {
		parsed, err := parseErrorRule(rule)
		if err != nil {
			return nil, err
		}
		backend.gone = append(backend.gone, parsed)
	}
	for _, rule := range def.Errors.MetaMissing {
		parsed, err := parseErrorRule(rule)
		if err != nil {
			return nil, err
		}
		backend.metaMissing = append(backend.metaMissing, parsed)
	}
	return backend, nil
}

func (b *DeclarativeBackend) Name() string          { return b.launcher }
func (b *DeclarativeBackend) Executable() string    { return b.def.Binary }
func (b *DeclarativeBackend) VersionArgs() []string { return append([]string{}, b.def.VersionArgs...) }

// AutoPriority orders definition launchers during auto resolution.
func (b *DeclarativeBackend) AutoPriority() int { return b.spec.AutoPriority }

func (b *DeclarativeBackend) Capabilities() Capabilities {
	c := b.def.Capabilities
	return Capabilities{
		Container:         c.Container,
		Focus:             c.Focus,
		PaneMetadata:      c.PaneMetadata,
		ForegroundProcess: c.ForegroundProcess,
		SessionReport:     c.SessionReport,
		WaitOutput:        c.WaitOutput,
		AgentIdentity:     c.AgentIdentity,
		POSIXOnly:         b.spec.Requires.Platform == "posix",
	}
}

func (b *DeclarativeBackend) op(name string) (Op, bool) {
	if op, ok := b.spec.Ops[name]; ok {
		return op, true
	}
	op, ok := b.def.Ops[name]
	return op, ok
}

func (b *DeclarativeBackend) OpaqueAddress(address Address) string {
	parts := make([]string, len(b.def.Address))
	for index, field := range b.def.Address {
		parts[index] = addressField(address, field.Name)
	}
	return strings.Join(parts, ":")
}

func addressField(address Address, name string) string {
	switch name {
	case "session":
		return address.Session
	case "container":
		return address.Container
	default:
		return address.Pane
	}
}

func (b *DeclarativeBackend) ParseAddress(value string) (Address, bool) {
	match := b.addressRe.FindStringSubmatch(value)
	if match == nil {
		return Address{}, false
	}
	var address Address
	for index, field := range b.def.Address {
		switch field.Name {
		case "session":
			address.Session = match[index+1]
		case "container":
			address.Container = match[index+1]
		default:
			address.Pane = match[index+1]
		}
	}
	return address, true
}

// ParseFocusAddress accepts a strict WINDOW value, or, for the read-only focus
// path, the same number of fields as the address with every field a plain
// colon-free segment (legacy IDs without a prefix such as a workspace).
func (b *DeclarativeBackend) ParseFocusAddress(fields []string) (Address, bool) {
	if address, ok := b.ParseAddress(strings.Join(fields, ":")); ok {
		return address, true
	}
	if len(fields) != len(b.def.Address)+1 || fields[0] != b.launcher {
		return Address{}, false
	}
	var address Address
	for index, field := range b.def.Address {
		value := fields[index+1]
		if !plainSegment.MatchString(value) {
			return Address{}, false
		}
		switch field.Name {
		case "session":
			address.Session = value
		case "container":
			address.Container = value
		default:
			address.Pane = value
		}
	}
	return address, true
}

var plainSegment = regexp.MustCompile(`^[^:\s]+$`)

// AutoDetect selects a launcher that runs inside an existing session when its
// platform and environment requirements hold.
func (b *DeclarativeBackend) AutoDetect(getenv func(string) string) bool {
	requires := b.spec.Requires
	if b.spec.AutoPriority <= 0 || !requires.InsideSession {
		return false
	}
	if requires.Platform == "windows" && runtime.GOOS != "windows" {
		return false
	}
	for _, requirement := range requires.Env {
		if !requirement.SkipAuto && !requirement.satisfied(getenv) {
			return false
		}
	}
	return true
}

func (r EnvRequirement) satisfied(getenv func(string) string) bool {
	value := getenv(r.Name)
	if r.Trim {
		value = strings.TrimSpace(value)
	}
	return value != "" && (r.Value == nil || value == *r.Value)
}

// launcherGetenv trims the variables the launcher requires with trim, so
// {env.<NAME>} sees the same value the precondition accepted.
func (b *DeclarativeBackend) launcherGetenv(getenv func(string) string) func(string) string {
	trimmed := map[string]bool{}
	for _, requirement := range b.spec.Requires.Env {
		if requirement.Trim {
			trimmed[requirement.Name] = true
		}
	}
	if len(trimmed) == 0 {
		return getenv
	}
	return func(name string) string {
		if trimmed[name] {
			return strings.TrimSpace(getenv(name))
		}
		return getenv(name)
	}
}

// ProjectKey derives the stable project token of a project path: a printable
// label from the directory name plus an eight-digit digest of the full path.
func ProjectKey(project string) string {
	base := project
	if normalized := strings.ReplaceAll(project, `\`, "/"); strings.Contains(normalized, "/") {
		base = normalized[strings.LastIndex(normalized, "/")+1:]
	}
	var label strings.Builder
	for _, r := range base {
		if !unicode.IsPrint(r) {
			continue
		}
		if unicode.IsSpace(r) || r == '.' || r == ':' {
			label.WriteByte('-')
			continue
		}
		label.WriteRune(r)
	}
	text := strings.Trim(label.String(), "-")
	text = regexp.MustCompile(`-{2,}`).ReplaceAllString(text, "-")
	sum := sha256.Sum256([]byte(project))
	digest := hex.EncodeToString(sum[:])[:8]
	if text == "" {
		return digest
	}
	if len([]rune(text)) > 30 {
		text = string([]rune(text)[:30])
	}
	return text + "-" + digest
}

// requireMessage renders a declared precondition message, or the catalog
// default with its arguments.
func (b *DeclarativeBackend) requireMessage(message *Message, names map[string]string, id string, args ...any) error {
	if message == nil {
		return &probe.Error{Message: config.Text(id, args...)}
	}
	e := b.newExecution(context.Background(), Conn{}, b.getenv, names)
	return &probe.Error{Message: e.render(message, nil)}
}

func (b *DeclarativeBackend) Prepare(request PrepareRequest) (Target, error) {
	return b.PrepareWithRunner(request, SpawnRunner)
}

// PrepareWithRunner keeps preflight commands observable and bounded for the
// conformance checker. Ordinary launch preparation retains SpawnRunner.
func (b *DeclarativeBackend) PrepareWithRunner(request PrepareRequest, runner Runner) (Target, error) {
	requires := b.spec.Requires
	names := map[string]string{"launcher": b.launcher, "binary": b.def.Binary, "platform": requires.Platform}
	if (requires.Platform == "posix" && request.Windows) || (requires.Platform == "windows" && !request.Windows) {
		return Target{}, b.requireMessage(requires.Messages.Platform, names, "terminal.platform_unsupported_"+requires.Platform, b.launcher)
	}
	getenv := b.launcherGetenv(request.Getenv)
	checkEnv := func(beforeBinary bool) error {
		for _, requirement := range requires.Env {
			if requirement.BeforeBinary != beforeBinary || requirement.satisfied(request.Getenv) {
				continue
			}
			names["name"] = requirement.Name
			message := requirement.Message
			if message == nil {
				message = requires.Messages.Env
			}
			return b.requireMessage(message, names, "terminal.env_missing", requirement.Name, b.launcher)
		}
		return nil
	}
	if err := checkEnv(true); err != nil {
		return Target{}, err
	}
	program := b.def.Binary
	if requires.Binary {
		path, err := request.LookPath(b.def.Binary)
		if err != nil {
			return Target{}, b.requireMessage(requires.Messages.Binary, names, "terminal.binary_not_found", b.def.Binary, b.launcher)
		}
		program = path
	}
	if err := checkEnv(false); err != nil {
		return Target{}, err
	}
	target := Target{Program: program, Project: request.Project}
	op, ok := b.op(OpPrepare)
	if !ok {
		return target, nil
	}
	conn := Conn{Program: program, Run: runner}
	e := b.newExecution(context.Background(), conn, getenv, map[string]string{
		"project": request.Project, "project_key": ProjectKey(request.Project), "command": request.Command,
	})
	result, err := e.runOp(op)
	if err != nil {
		return Target{}, err
	}
	target.Session = result["session"]
	target.SessionExists = result["session_exists"] == "true"
	target.Workspace = result["workspace"]
	return target, nil
}

func (b *DeclarativeBackend) StartedLines(head string, target Target, address Address, getenv func(string) string) []string {
	e := b.newExecution(context.Background(), Conn{}, b.launcherGetenv(getenv), map[string]string{
		"head": head, "session": target.Session, "session_exists": strconv.FormatBool(target.SessionExists),
		"workspace": target.Workspace, "project": target.Project, "container": address.Container, "pane": address.Pane,
	})
	var lines []string
	for index := range b.spec.StartedLines {
		line := &b.spec.StartedLines[index]
		if e.holds(line.When) {
			lines = append(lines, e.render(&line.Message, nil))
		}
	}
	return lines
}

// launchOp runs a launch-time operation without a deadline, like the other
// start steps.
func (b *DeclarativeBackend) launchOp(name string, conn Conn, inputs map[string]string) (map[string]string, error) {
	op, _ := b.op(name)
	return b.newExecution(context.Background(), conn, b.getenv, inputs).runOp(op)
}

func (b *DeclarativeBackend) CreateContainer(conn Conn, target Target, cwd, label string) (Address, error) {
	result, err := b.launchOp(OpCreateContainer, conn, map[string]string{
		"session": target.Session, "session_exists": strconv.FormatBool(target.SessionExists), "workspace": target.Workspace,
		"project": target.Project, "project_key": ProjectKey(target.Project), "cwd": cwd, "label": label,
	})
	if err != nil {
		return Address{}, err
	}
	address := Address{Session: result["session"], Container: result["container"], Pane: result["pane"]}
	if address.Container == "" || address.Pane == "" {
		return Address{}, &probe.Error{Message: config.Text("terminal.container_address_missing", b.launcher)}
	}
	return address, nil
}

func (b *DeclarativeBackend) WaitReady(conn Conn, pane string) error {
	if _, ok := b.op(OpWaitReady); !ok {
		return nil
	}
	_, err := b.launchOp(OpWaitReady, conn, map[string]string{"pane": pane})
	return err
}

func (b *DeclarativeBackend) RunCommand(conn Conn, pane, command string, posix bool) error {
	_, err := b.launchOp(OpRunCommand, conn, map[string]string{"pane": pane, "command": command, "posix": strconv.FormatBool(posix)})
	return err
}

func (b *DeclarativeBackend) SetSessionMarker(conn Conn, pane, value string) error {
	if _, ok := b.op(OpSetSessionMarker); !ok {
		return ErrUnsupported
	}
	_, err := b.launchOp(OpSetSessionMarker, conn, map[string]string{"pane": pane, "value": value})
	return err
}

// ReportSession calls the report_session hook once; a degraded hook means the
// report has no channel.
func (b *DeclarativeBackend) ReportSession(report SessionReport) error {
	hook, ok := b.hook(HookPointReportSession)
	if !ok {
		return ErrUnsupported
	}
	ctx := context.Background()
	if !report.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, report.Deadline)
		defer cancel()
	}
	result := runHook(ctx, hook, HookCall{
		Launcher: b.launcher, Point: HookPointReportSession, Conn: report.Conn, Getenv: b.getenv,
		Values: map[string]string{"pane": report.Pane, "agent": report.Agent, "reference": report.Reference}, Report: &report,
	})
	switch result.Status {
	case HookOK:
		return nil
	case HookDegraded:
		return fmt.Errorf("%w: %s", ErrNoReportChannel, result.Note)
	default:
		return result.Err
	}
}

func (b *DeclarativeBackend) hook(point string) (Hook, bool) {
	name := b.def.Hooks[point]
	if name == "" {
		return nil, false
	}
	return LookupHook(name)
}

func (b *DeclarativeBackend) probeOp(ctx context.Context, name string, conn Conn, inputs map[string]string, goneAware bool) (map[string]string, error) {
	op, _ := b.op(name)
	e := b.newExecution(ctx, conn, b.getenv, inputs)
	e.goneAware = goneAware
	return e.runOp(op)
}

// PaneFacts runs the pane_facts operation within the default probe budget; a
// failure classified as gone yields the gone fact.
func (b *DeclarativeBackend) PaneFacts(ctx context.Context, conn Conn, pane string) (PaneFacts, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	result, err := b.probeOp(ctx, OpPaneFacts, conn, map[string]string{"pane": pane}, true)
	if gone, ok := isGone(err); ok {
		return PaneFacts{Gone: true, GoneDetail: gone.detail}, nil
	}
	if err != nil {
		return PaneFacts{}, err
	}
	return PaneFacts{
		Command: result["command"], InMode: result["in_mode"], Dead: result["dead"], SessionMarker: result["session_marker"],
		Agent: result["agent"], AgentStatus: result["agent_status"], AgentSession: result["agent_session"], Container: result["container"],
	}, nil
}

func (b *DeclarativeBackend) ReadOutput(ctx context.Context, conn Conn, pane string) (string, error) {
	result, err := b.probeOp(ctx, OpReadOutput, conn, map[string]string{"pane": pane}, false)
	if err != nil {
		return "", err
	}
	return result["text"], nil
}

func (b *DeclarativeBackend) WaitOutput(ctx context.Context, conn Conn, pane, match string, timeoutMS int) error {
	if _, ok := b.op(OpWaitOutput); !ok {
		return ErrUnsupported
	}
	// marker_literal and marker_regex split the Backend match contract (a
	// literal, or a "regex:"-prefixed expression) so a definition can pass
	// each to its own flag; the empty one drops together with its flag.
	literal, regex := match, ""
	if pattern, ok := strings.CutPrefix(match, "regex:"); ok {
		literal, regex = "", pattern
	}
	_, err := b.probeOp(ctx, OpWaitOutput, conn, map[string]string{
		"pane": pane, "marker": match, "marker_literal": literal, "marker_regex": regex, "timeout_ms": strconv.Itoa(timeoutMS),
	}, false)
	return err
}

func (b *DeclarativeBackend) DeliverText(ctx context.Context, conn Conn, pane, text string) error {
	_, err := b.probeOp(ctx, OpDeliverText, conn, map[string]string{"pane": pane, "text": text}, false)
	return err
}

func addressInputs(address Address) map[string]string {
	return map[string]string{"session": address.Session, "container": address.Container, "pane": address.Pane}
}

func (b *DeclarativeBackend) Topology(ctx context.Context, conn Conn, address Address) (Topology, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	op, _ := b.op(OpTopology)
	e := b.newExecution(ctx, conn, b.getenv, addressInputs(address))
	result, err := e.runOp(op)
	if err != nil {
		return Topology{}, err
	}
	topology := Topology{Session: result["session"], Container: result["container"], PaneCount: result["pane_count"]}
	if op.Rows != nil {
		rows, err := e.matchRows(op.Rows)
		if err != nil {
			return Topology{}, err
		}
		for _, row := range rows {
			topology.Panes = append(topology.Panes, row["pane"])
		}
	}
	return topology, nil
}

// ContainerExists reports false when the probe failure is classified as gone.
func (b *DeclarativeBackend) ContainerExists(ctx context.Context, conn Conn, address Address) (bool, error) {
	_, err := b.probeOp(ctx, OpContainerExists, conn, addressInputs(address), true)
	if _, ok := isGone(err); ok {
		return false, nil
	}
	return err == nil, err
}

// ReverseLookup runs the collection steps, resolves the expected process name
// only after they succeed, then requires exactly one matching row.
func (b *DeclarativeBackend) ReverseLookup(ctx context.Context, conn Conn, identity Identity) (Address, error) {
	ctx, cancel := probe.WithDefaultTimeout(ctx)
	defer cancel()
	op, _ := b.op(OpReverseLookup)
	e := b.newExecution(ctx, conn, b.getenv, map[string]string{"agent": identity.Agent, "reference": identity.Reference})
	if err := e.runSteps(op.Steps); err != nil {
		return Address{}, err
	}
	// The expected process name loads the agent definition; resolve it only
	// when the rows reference it.
	if identity.ProcessName != nil && rowsReference(op.Rows, "process_name") {
		name, err := identity.ProcessName()
		if err != nil {
			return Address{}, err
		}
		e.values["process_name"] = name
	}
	result, err := e.uniqueRow(op.Rows)
	if err != nil {
		return Address{}, err
	}
	return Address{Session: result["session"], Container: result["container"], Pane: result["pane"]}, nil
}

// Focus checks availability, probes the pane, runs the container switch steps
// and then an optional focus_pane hook whose degraded result still counts as
// switched.
func (b *DeclarativeBackend) Focus(ctx context.Context, conn Conn, address Address) FocusResult {
	op, _ := b.op(OpFocus)
	e := b.newExecution(ctx, conn, b.getenv, addressInputs(address))
	for index := range op.Unavailable {
		line := &op.Unavailable[index]
		if e.holds(line.When) {
			return textResult(false, line.Message, e)
		}
	}
	facts, err := b.PaneFacts(ctx, conn, address.Pane)
	if err != nil {
		return FocusResult{ID: "focus.probe_failed", Args: []any{probe.FailureDetail(err)}}
	}
	e.values["facts.command"], e.values["facts.in_mode"] = facts.Command, facts.InMode
	e.values["facts.dead"], e.values["facts.session_marker"] = facts.Dead, facts.SessionMarker
	e.values["facts.agent"], e.values["facts.agent_status"] = facts.Agent, facts.AgentStatus
	e.values["facts.agent_session"], e.values["facts.container"] = facts.AgentSession, facts.Container
	if facts.Gone || (op.ClosedWhen != nil && e.holds(op.ClosedWhen)) {
		return FocusResult{ID: "focus.closed"}
	}
	// The switch steps run with the full step machinery; a failure without
	// its own message keeps the focus step diagnostic, and a failure an
	// on_error policy continued past degrades the result.
	e.focus = true
	if err := e.runSteps(op.Steps); err != nil {
		detail := probe.FailureDetail(err)
		if commandErr, ok := AsCommandError(err); ok {
			detail = commandErr.Message
		}
		return FocusResult{ID: "focus.switch_failed", Args: []any{detail}}
	}
	if len(e.continued) > 0 {
		return FocusResult{Success: true, ID: "focus.tab_only", Args: []any{e.continued[0]}}
	}
	if hook, ok := b.hook(HookPointFocusPane); ok {
		result := runHook(ctx, hook, HookCall{Launcher: b.launcher, Point: HookPointFocusPane, Conn: conn, Getenv: b.getenv, Values: addressInputs(address)})
		switch result.Status {
		case HookDegraded:
			return FocusResult{Success: true, ID: "focus.tab_only", Args: []any{result.Note}}
		case HookFailed:
			return FocusResult{ID: "focus.switch_failed", Args: []any{probe.FailureDetail(result.Err)}}
		}
	}
	return FocusResult{Success: true, ID: "focus.success"}
}

// textResult turns a definition message into a focus notice: a catalog
// message keeps its ID, a template renders through an ID that prints its
// single argument verbatim.
func textResult(success bool, message Message, e *execution) FocusResult {
	if message.ID != "" {
		args := make([]any, len(message.Args))
		for index := range message.Args {
			args[index] = e.render(&message.Args[index], nil)
		}
		return FocusResult{Success: success, ID: message.ID, Args: args}
	}
	return FocusResult{Success: success, ID: "terminal.text", Args: []any{e.render(&message, nil)}}
}

func (b *DeclarativeBackend) CloseContainer(ctx context.Context, conn Conn, address Address) error {
	_, err := b.probeOp(ctx, OpCloseContainer, conn, addressInputs(address), false)
	return err
}

// rowsReference reports whether the row conditions or results use a name.
func rowsReference(rows *Rows, name string) bool {
	uses := func(set Conditions) bool {
		for _, group := range set {
			for _, condition := range group {
				if strings.Contains(condition, name) {
					return true
				}
			}
		}
		return false
	}
	if uses(rows.Expect) || uses(rows.Match) {
		return true
	}
	for _, check := range rows.Checks {
		if uses(check.When) || messageUses(&check.Message, name) {
			return true
		}
	}
	for _, template := range rows.Result {
		if strings.Contains(template, "{"+name+"}") {
			return true
		}
	}
	for _, message := range []*Message{rows.Messages.Missing, rows.Messages.Invalid, rows.Messages.None, rows.Messages.Ambiguous} {
		if message != nil && messageUses(message, name) {
			return true
		}
	}
	return false
}

// messageUses reports whether a message template, argument or alternative
// uses a placeholder name.
func messageUses(message *Message, name string) bool {
	if strings.Contains(message.Template, "{"+name+"}") {
		return true
	}
	for index := range message.Args {
		if messageUses(&message.Args[index], name) {
			return true
		}
	}
	for index := range message.Or {
		if messageUses(&message.Or[index], name) {
			return true
		}
	}
	return false
}
