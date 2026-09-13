package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
)

// errorRule is one parsed errors.gone or errors.meta_missing rule.
type errorRule struct {
	exitCode int
	stream   string
	pattern  *regexp.Regexp
	jsonPath string
	value    string
}

func parseErrorRule(rule string) (errorRule, error) {
	kind, body, ok := strings.Cut(rule, ":")
	if !ok {
		return errorRule{}, fmt.Errorf("rule %q must be exit:<code>, stderr:<regex>, stdout_json:<path>=<value> or stderr_json:<path>=<value>", rule)
	}
	switch kind {
	case "exit":
		code, err := strconv.Atoi(body)
		if err != nil {
			return errorRule{}, fmt.Errorf("rule %q: exit code must be an integer", rule)
		}
		return errorRule{exitCode: code}, nil
	case "stderr":
		pattern, err := regexp.Compile(body)
		if err != nil {
			return errorRule{}, fmt.Errorf("rule %q: regex does not compile: %s", rule, err.Error())
		}
		return errorRule{stream: process.SourceStderr, pattern: pattern}, nil
	case "stdout_json", "stderr_json":
		path, value, ok := strings.Cut(body, "=")
		spec := process.OutputSpec{Source: process.SourceStdout, Parse: "json_field:" + path}
		if !ok || process.ValidateTerminalOutput(spec) != nil {
			return errorRule{}, fmt.Errorf("rule %q must be %s:<dotted.path>=<value>", rule, kind)
		}
		return errorRule{stream: strings.TrimSuffix(kind, "_json"), jsonPath: path, value: value}, nil
	default:
		return errorRule{}, fmt.Errorf("rule %q has an unknown kind %q", rule, kind)
	}
}

// matchAny classifies a non-zero exit when any rule matches. The stderr regex
// sees the trimmed stderr. JSON rules on the same dotted path share one
// resolved code: the first stream, in the order the rules name the streams,
// whose output holds a non-empty string there. A code in stderr therefore
// decides even when stdout carries another one.
func matchAny(rules []errorRule, result probe.Result) bool {
	codes := map[string]string{}
	for _, rule := range rules {
		switch {
		case rule.pattern != nil:
			if rule.pattern.MatchString(strings.TrimSpace(result.Stderr)) {
				return true
			}
		case rule.jsonPath != "":
			code, resolved := codes[rule.jsonPath]
			if !resolved {
				code = resolveCode(rules, rule.jsonPath, result)
				codes[rule.jsonPath] = code
			}
			if code != "" && code == rule.value {
				return true
			}
		default:
			if result.Code == rule.exitCode {
				return true
			}
		}
	}
	return false
}

func resolveCode(rules []errorRule, path string, result probe.Result) string {
	for _, rule := range rules {
		if rule.jsonPath != path {
			continue
		}
		text := result.Stdout
		if rule.stream == process.SourceStderr {
			text = result.Stderr
		}
		code, err := process.ParseTerminalOutput(process.OutputSpec{Source: rule.stream, Parse: "json_field:" + path}, text)
		if err == nil && code != "" {
			return code
		}
	}
	return ""
}

// goneSignal ends a gone-aware operation with the gone fact.
type goneSignal struct{ detail string }

func (g *goneSignal) Error() string { return "terminal: pane is gone: " + g.detail }

// execution runs one operation of a declarative backend.
type execution struct {
	backend   *DeclarativeBackend
	ctx       context.Context
	conn      Conn
	getenv    func(string) string
	values    map[string]string
	goneAware bool
	executed  int
	prevRan   bool
	prevOK    bool
	// focus renders unmessaged step failures as focus step diagnostics and
	// records failures that on_error continued past.
	focus     bool
	continued []string
}

func (b *DeclarativeBackend) newExecution(ctx context.Context, conn Conn, getenv func(string) string, inputs map[string]string) *execution {
	values := make(map[string]string, len(inputs))
	for name, value := range inputs {
		values[name] = value
	}
	return &execution{backend: b, ctx: ctx, conn: conn, getenv: getenv, values: values}
}

func (e *execution) value(name string) string {
	if env, ok := strings.CutPrefix(name, "env."); ok {
		return e.getenv(env)
	}
	return e.values[name]
}

func (e *execution) valueMap(names []string, extra map[string]string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		if value, ok := extra[name]; ok {
			out[name] = value
			continue
		}
		out[name] = e.value(name)
	}
	return out
}

// expand renders a validated text template.
func (e *execution) expand(template string, extra map[string]string) string {
	names, err := process.TemplatePlaceholders(template)
	if err != nil {
		return template
	}
	text, err := process.ExpandTemplate(template, e.valueMap(names, extra), names)
	if err != nil {
		return template
	}
	return text
}

func (e *execution) expandArgv(argv []string) ([]string, error) {
	var names []string
	for _, element := range argv {
		elementNames, err := process.TemplatePlaceholders(element)
		if err != nil {
			return nil, err
		}
		names = append(names, elementNames...)
	}
	values := e.valueMap(names, nil)
	for _, name := range names {
		if strings.ContainsAny(values[name], "\r\n\x00") {
			return nil, &probe.Error{Message: config.Text("terminal.value_contains_line_break", name)}
		}
	}
	return process.ExpandTerminalArgv(argv, values, names)
}

func (e *execution) holds(set Conditions) bool {
	if len(set) == 0 {
		return true
	}
	for _, group := range set {
		all := true
		for _, condition := range group {
			if !e.condition(condition) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func (e *execution) condition(condition string) bool {
	body, negated := strings.CutPrefix(condition, "!")
	var result bool
	switch {
	case body == "prev_ok":
		result = e.prevRan && e.prevOK
	case body == "prev_failed":
		result = e.prevRan && !e.prevOK
	case strings.HasPrefix(body, "field_missing:"):
		result = e.value(strings.TrimPrefix(body, "field_missing:")) == ""
	default:
		name, template, _ := strings.Cut(strings.TrimPrefix(body, "field:"), "=")
		result = e.value(name) == e.expand(template, nil)
	}
	return result != negated
}

func (e *execution) render(message *Message, extra map[string]string) string {
	switch {
	case message == nil:
		return ""
	case message.Or != nil:
		for index := range message.Or {
			if text := e.render(&message.Or[index], extra); text != "" {
				return text
			}
		}
		return ""
	case message.ID != "":
		args := make([]any, len(message.Args))
		for index := range message.Args {
			args[index] = e.render(&message.Args[index], extra)
		}
		return config.Text(message.ID, args...)
	default:
		return e.expand(message.Template, extra)
	}
}

// renderOr renders a declared message, or a catalog default without one.
func (e *execution) renderOr(message *Message, extra map[string]string, defaultID string, args ...any) string {
	if message == nil {
		return config.Text(defaultID, args...)
	}
	return e.render(message, extra)
}

// store replaces a named result, so a later step with the same store name
// supersedes an earlier attempt (for example a legacy fallback read).
func (e *execution) store(name string, fields map[string]string) {
	if name == "" {
		return
	}
	prefix := "step." + name + "."
	for key := range e.values {
		if strings.HasPrefix(key, prefix) {
			delete(e.values, key)
		}
	}
	for field, value := range fields {
		e.values[prefix+field] = value
	}
}

func (e *execution) snapshot() map[string]string {
	out := make(map[string]string, len(e.values))
	for name, value := range e.values {
		out[name] = value
	}
	return out
}

// runSteps runs a step list; stop_on_success skips the rest of this list.
func (e *execution) runSteps(steps []Step) error {
	for index := range steps {
		step := &steps[index]
		if !e.holds(step.When) {
			continue
		}
		if step.Fail != nil {
			return &probe.Error{Message: e.render(step.Fail, nil)}
		}
		stop, err := e.runStep(step)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return nil
}

// stepFailure is a command step that did not succeed.
type stepFailure struct {
	kind   ErrorKind
	cause  error
	result probe.Result
	detail string
	argv   []string
}

func (e *execution) failureNames(failure *stepFailure) map[string]string {
	errText := strings.TrimSpace(failure.result.Stderr)
	if failure.kind == KindExec {
		errText = failure.cause.Error()
	}
	return map[string]string{"detail": failure.detail, "error": errText, "output": strings.TrimSpace(failure.result.Stdout)}
}

func (e *execution) runStep(step *Step) (bool, error) {
	if e.executed > 0 {
		if err := e.ctx.Err(); err != nil {
			return false, err
		}
	}
	e.executed++
	argv, err := e.expandArgv(step.Argv)
	if err != nil {
		// A rejected runtime value is a failure of this step, so on_error
		// still decides whether the operation goes on.
		return false, e.stepFailed(step, &stepFailure{kind: KindExec, cause: err, detail: err.Error()})
	}
	ctx := e.ctx
	if step.Timeout != "" {
		timeout, _ := positiveDuration(step.Timeout)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	result, text, fields, failure := e.attempt(ctx, step, argv)
	if failure != nil {
		failure.argv = argv
		return false, e.stepFailed(step, failure)
	}
	stored := map[string]string{"ok": "true", "text": text}
	for name, value := range fields {
		stored[name] = value
	}
	e.store(step.Store, stored)
	if e.goneAware && step.GoneWhen != nil && e.holds(step.GoneWhen) {
		return false, &goneSignal{detail: e.renderOr(step.Messages.Gone, nil, "terminal.target_reported_gone")}
	}
	if !e.holds(step.Expect) {
		failure := &stepFailure{kind: KindInvalidResponse, result: result, detail: config.Text("terminal.output_does_not_match")}
		return false, &CommandError{Kind: KindInvalidResponse, Message: e.stepMessage(step.Messages.Invalid, failure), Stderr: result.Stderr}
	}
	e.prevRan, e.prevOK = true, true
	return step.StopOnSuccess, nil
}

// attempt runs the command, repeating it while a poll condition is unmet.
func (e *execution) attempt(ctx context.Context, step *Step, argv []string) (probe.Result, string, map[string]string, *stepFailure) {
	var deadline time.Time
	var interval time.Duration
	if step.Poll != nil {
		interval, _ = positiveDuration(step.Poll.Interval)
		timeout := e.pollTimeout(step.Poll)
		deadline = time.Now().Add(timeout)
	}
	for {
		result, runErr := e.conn.Run(ctx, e.conn.Program, argv)
		if runErr != nil {
			return result, "", nil, &stepFailure{kind: KindExec, cause: runErr, result: result, detail: runErr.Error()}
		}
		if result.Code != 0 {
			detail := strings.TrimSpace(result.Stderr)
			if detail == "" {
				detail = "exit " + strconv.Itoa(result.Code)
			}
			return result, "", nil, &stepFailure{kind: KindExit, result: result, detail: detail}
		}
		text, parseKind, parseErr := extractOutput(step.Output, result)
		if step.Poll == nil {
			if parseErr != nil {
				return result, "", nil, &stepFailure{kind: parseKind, result: result, detail: parseErr.Error()}
			}
			return result, text, extractFields(step, text), nil
		}
		if parseErr == nil && e.pollSatisfied(step.Poll, text) {
			return result, text, extractFields(step, text), nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			detail := config.Text("terminal.poll_timed_out", step.Poll.Until)
			return result, "", nil, &stepFailure{kind: KindExit, result: result, detail: detail}
		}
		timer := time.NewTimer(min(interval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, "", nil, &stepFailure{kind: KindExec, cause: ctx.Err(), result: result, detail: ctx.Err().Error()}
		case <-timer.C:
		}
	}
}

func (e *execution) pollTimeout(poll *Poll) time.Duration {
	if poll.Timeout == "{timeout_ms}" {
		ms, err := strconv.Atoi(e.values["timeout_ms"])
		if err != nil || ms <= 0 {
			return 0
		}
		return time.Duration(ms) * time.Millisecond
	}
	timeout, _ := positiveDuration(poll.Timeout)
	return timeout
}

func (e *execution) pollSatisfied(poll *Poll, text string) bool {
	switch {
	case poll.Until == "nonempty":
		return text != ""
	case poll.Until == "matched":
		marker := e.values["marker"]
		if pattern, ok := strings.CutPrefix(marker, "regex:"); ok {
			re, err := regexp.Compile(pattern)
			return err == nil && re.MatchString(text)
		}
		return strings.Contains(text, marker)
	default:
		path, want, _ := strings.Cut(strings.TrimPrefix(poll.Until, "json_field:"), "=")
		got, err := process.ParseTerminalOutput(process.OutputSpec{Source: process.SourceStdout, Parse: "json_field:" + path}, text)
		return err == nil && got == want
	}
}

// extractOutput applies the output spec through the shared parser. When a
// json_field output fails, the failure is classified like a JSON API
// response: not JSON, not an object, or the path missing; a path that holds
// an object or array yields that sub-document as compact JSON with sorted keys.
func extractOutput(spec *process.OutputSpec, result probe.Result) (string, ErrorKind, error) {
	if spec == nil {
		return result.Stdout, 0, nil
	}
	data := result.Stdout
	if spec.Source == process.SourceStderr {
		data = result.Stderr
	}
	text, err := process.ParseTerminalOutput(*spec, data)
	path, isJSONField := strings.CutPrefix(spec.Parse, "json_field:")
	if err == nil || !isJSONField || errors.Is(err, process.ErrSuccess) {
		return text, KindInvalidResponse, err
	}
	var doc any
	if decodeErr := json.Unmarshal([]byte(data), &doc); decodeErr != nil {
		return "", KindNotJSON, decodeErr
	}
	if _, ok := doc.(map[string]any); !ok {
		return "", KindNotObject, err
	}
	for _, key := range strings.Split(path, ".") {
		object, ok := doc.(map[string]any)
		if !ok {
			return "", KindMissingResult, err
		}
		if doc, ok = object[key]; !ok {
			return "", KindMissingResult, err
		}
	}
	switch doc.(type) {
	case map[string]any, []any:
		encoded, encodeErr := json.Marshal(doc)
		if encodeErr != nil {
			return "", KindInvalidResponse, encodeErr
		}
		return string(encoded), 0, nil
	default:
		return "", KindMissingResult, err
	}
}

// extractFields applies each field primitive to the output text; a field whose
// primitive does not match stays absent.
func extractFields(step *Step, text string) map[string]string {
	source := process.SourceStdout
	if step.Output != nil {
		source = step.Output.Source
	}
	return parseFields(step.Fields, source, text)
}

func parseFields(fields map[string]string, source, text string) map[string]string {
	out := make(map[string]string, len(fields))
	for name, parse := range fields {
		if value, err := process.ParseTerminalOutput(process.OutputSpec{Source: source, Parse: parse}, text); err == nil {
			out[name] = value
		}
	}
	return out
}

// stepFailed applies the error classification and on_error policy.
func (e *execution) stepFailed(step *Step, failure *stepFailure) error {
	if failure.kind == KindExit && failure.result.Code != 0 {
		if e.goneAware && matchAny(e.backend.gone, failure.result) {
			return &goneSignal{detail: failure.detail}
		}
	}
	metaMissing := failure.kind == KindExit && failure.result.Code != 0 && matchAny(e.backend.metaMissing, failure.result)
	switch {
	case step.OnError == OnErrorContinue, step.OnError == OnErrorMetaMissing && metaMissing:
		e.store(step.Store, map[string]string{"ok": "false", "detail": failure.detail})
		e.prevRan, e.prevOK = true, false
		if e.focus {
			e.continued = append(e.continued, e.focusDetail(failure))
		}
		return nil
	}
	if e.focus && step.Messages == (StepMessages{}) {
		return &CommandError{Kind: failure.kind, Message: e.focusDetail(failure), Cause: failure.cause, Code: failure.result.Code, Stderr: failure.result.Stderr}
	}
	switch failure.kind {
	case KindExec:
		message := failure.cause.Error()
		if step.Messages.Exec != nil {
			message = e.stepMessage(step.Messages.Exec, failure)
		}
		return &CommandError{Kind: KindExec, Message: message, Cause: failure.cause, Code: failure.result.Code, Stderr: failure.result.Stderr}
	case KindInvalidResponse, KindNotJSON, KindNotObject, KindMissingResult:
		return &CommandError{Kind: failure.kind, Message: e.stepMessage(step.Messages.forKind(failure.kind), failure), Cause: failure.cause, Code: failure.result.Code, Stderr: failure.result.Stderr}
	default:
		return &CommandError{Kind: KindExit, Message: e.stepMessage(step.Messages.Exit, failure), Code: failure.result.Code, Stderr: failure.result.Stderr}
	}
}

// focusDetail renders a failed focus step as "<subcommand>: <detail>",
// preferring the run error, then stderr, then stdout, then the exit status.
func (e *execution) focusDetail(failure *stepFailure) string {
	subcommand := ""
	if len(failure.argv) > 0 {
		subcommand = failure.argv[0] + ": "
	}
	if failure.kind == KindExec && failure.cause != nil {
		return subcommand + probe.FailureDetail(failure.cause)
	}
	if failure.kind != KindExit || failure.result.Code == 0 {
		return subcommand + failure.detail
	}
	detail := strings.TrimSpace(failure.result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(failure.result.Stdout)
	}
	if detail == "" {
		detail = failure.detail
	}
	return subcommand + detail
}

// stepMessage renders a step message, or the raw detail without one.
func (e *execution) stepMessage(message *Message, failure *stepFailure) string {
	if message == nil {
		return failure.detail
	}
	return e.render(message, e.failureNames(failure))
}

// runOp runs the steps, candidates and rows of an operation and returns its
// result fields.
func (e *execution) runOp(op Op) (map[string]string, error) {
	if err := e.runSteps(op.Steps); err != nil {
		return nil, err
	}
	result := map[string]string{}
	if op.Candidates != nil {
		picked, err := e.runCandidates(op.Candidates)
		if err != nil {
			return nil, err
		}
		for key, value := range picked {
			result[key] = value
		}
	}
	for key, template := range op.Result {
		result[key] = e.expand(template, nil)
	}
	return result, nil
}

func (e *execution) runCandidates(candidates *Candidates) (map[string]string, error) {
	for _, template := range candidates.Values {
		saved := e.snapshot()
		e.values["candidate"] = e.expand(template, nil)
		e.prevRan, e.prevOK = false, false
		if err := e.runSteps(candidates.Steps); err != nil {
			return nil, err
		}
		for _, entry := range candidates.Select {
			if !e.holds(entry.When) {
				continue
			}
			picked := map[string]string{}
			for key, value := range entry.Result {
				picked[key] = e.expand(value, nil)
			}
			return picked, nil
		}
		e.values = saved
	}
	e.prevRan, e.prevOK = false, false
	return nil, e.runSteps(candidates.Fallback)
}

// rowTexts splits the stored output into rows: non-blank lines, or the
// elements of a JSON array re-encoded as compact JSON with sorted keys, so
// field primitives see one deterministic text per row.
func (e *execution) rowTexts(rows *Rows) ([]string, error) {
	text := e.values["step."+rows.From+".text"]
	path, isArray := strings.CutPrefix(rows.Split, "json_array:")
	if !isArray {
		var out []string
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
		}
		return out, nil
	}
	var doc any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return nil, &probe.Error{Message: e.renderOr(rows.Messages.Missing, nil, "terminal.rows_missing")}
	}
	for _, key := range strings.Split(path, ".") {
		object, _ := doc.(map[string]any)
		doc = object[key]
	}
	elements, ok := doc.([]any)
	if !ok {
		return nil, &probe.Error{Message: e.renderOr(rows.Messages.Missing, nil, "terminal.rows_missing")}
	}
	out := make([]string, len(elements))
	for index, element := range elements {
		encoded, err := json.Marshal(element)
		if err != nil {
			return nil, err
		}
		out[index] = string(encoded)
	}
	return out, nil
}

// matchRows validates every row and returns the results of the matching rows.
func (e *execution) matchRows(rows *Rows) ([]map[string]string, error) {
	texts, err := e.rowTexts(rows)
	if err != nil {
		return nil, err
	}
	var matches []map[string]string
	for _, text := range texts {
		if err := e.ctx.Err(); err != nil {
			return nil, err
		}
		fields := parseFields(rows.Fields, process.SourceStdout, text)
		for name := range rows.Fields {
			e.values["row."+name] = fields[name]
		}
		if !e.holds(rows.Expect) {
			return nil, &probe.Error{Message: e.renderOr(rows.Messages.Invalid, nil, "terminal.rows_invalid")}
		}
		for index := range rows.Checks {
			if check := &rows.Checks[index]; !e.holds(check.When) {
				return nil, &probe.Error{Message: e.render(&check.Message, nil)}
			}
		}
		if !e.holds(rows.Match) {
			continue
		}
		result := map[string]string{}
		for key, template := range rows.Result {
			result[key] = e.expand(template, nil)
		}
		matches = append(matches, result)
	}
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

// uniqueRow requires exactly one matching row (reverse lookup).
func (e *execution) uniqueRow(rows *Rows) (map[string]string, error) {
	matches, err := e.matchRows(rows)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, &MatchError{Matches: 0, Cause: &probe.Error{Message: e.renderOr(rows.Messages.None, nil, "terminal.no_match")}}
	default:
		count := strconv.Itoa(len(matches))
		return nil, &MatchError{Matches: len(matches), Cause: &probe.Error{Message: e.renderOr(rows.Messages.Ambiguous, map[string]string{"count": count}, "terminal.ambiguous_match", count)}}
	}
}

func isGone(err error) (*goneSignal, bool) {
	var gone *goneSignal
	ok := errors.As(err, &gone)
	return gone, ok
}
