package terminal

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/process"
)

var (
	definitionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	storeNamePattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	envNamePattern        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// implicitStoreFields are set on every stored step result.
var implicitStoreFields = []string{"ok", "detail", "text"}

// stepMessageNames are available only while a failed step renders its message.
var stepMessageNames = []string{"detail", "error", "output"}

// opInputs are the placeholder names each operation receives from its Backend
// method arguments.
var opInputs = map[string][]string{
	OpPrepare:          {"project", "project_key", "command"},
	OpCreateContainer:  {"session", "session_exists", "workspace", "project", "project_key", "cwd", "label"},
	OpWaitReady:        {"pane"},
	OpRunCommand:       {"pane", "command", "posix"},
	OpSetSessionMarker: {"pane", "value"},
	OpPaneFacts:        {"pane"},
	OpReadOutput:       {"pane"},
	OpWaitOutput:       {"pane", "marker", "marker_literal", "marker_regex", "timeout_ms"},
	OpDeliverText:      {"pane", "text"},
	OpTopology:         {"session", "container", "pane"},
	OpContainerExists:  {"session", "container", "pane"},
	OpReverseLookup:    {"agent", "reference", "process_name"},
	OpFocus:            {"session", "container", "pane"},
	OpCloseContainer:   {"session", "container", "pane"},
}

// opResults are the result fields each operation maps.
var opResults = map[string][]string{
	OpPrepare:         {"session", "session_exists", "workspace"},
	OpCreateContainer: {"session", "container", "pane"},
	OpPaneFacts:       {"command", "in_mode", "dead", "session_marker", "agent", "agent_status", "agent_session", "container"},
	OpReadOutput:      {"text"},
	OpTopology:        {"session", "container", "pane_count"},
	OpReverseLookup:   {"session", "container", "pane"},
}

var startedLineNames = []string{"head", "session", "session_exists", "workspace", "project", "container", "pane"}

var paneFactNames = []string{
	"facts.command", "facts.in_mode", "facts.dead", "facts.session_marker",
	"facts.agent", "facts.agent_status", "facts.agent_session", "facts.container",
}

// rowResults are the per-row result fields of the operations that scan rows.
var rowResults = map[string][]string{
	OpReverseLookup: {"session", "container", "pane"},
	OpTopology:      {"pane"},
}

// ValidateDefinition checks a decoded definition. Hooks named by the
// definition must already be registered.
func ValidateDefinition(source string, def *Definition) error {
	v := validator{source: source}
	if def.SchemaVersion != DefinitionSchemaVersion {
		return v.fieldErr("schema_version", "unknown schema version %d (this binary reads %d)", def.SchemaVersion, DefinitionSchemaVersion)
	}
	if !definitionNamePattern.MatchString(def.Name) {
		return v.fieldErr("name", "must match %s", definitionNamePattern)
	}
	if base := strings.TrimSuffix(filepath.Base(source), ".json"); strings.HasSuffix(source, ".json") && base != def.Name {
		return v.fieldErr("name", "%q does not match the file name %q", def.Name, base)
	}
	if err := validateBinary(def.Binary); err != nil {
		return v.fieldErr("binary", "%s", err.Error())
	}
	for index, element := range def.VersionArgs {
		if placeholders, err := process.TemplatePlaceholders(element); err != nil || len(placeholders) > 0 {
			return v.fieldErr("version_args["+strconv.Itoa(index)+"]", "must be literal: version arguments take no placeholders")
		}
	}
	if err := v.argv("version_args", def.VersionArgs, newScope()); err != nil {
		return err
	}
	if !def.Capabilities.Container {
		return v.fieldErr("capabilities.container", "must be true: a definition provides container launchers")
	}
	if err := v.address(def.Address); err != nil {
		return err
	}
	if err := v.errorRules(def.Errors); err != nil {
		return err
	}
	if err := v.hooks(def); err != nil {
		return err
	}
	if len(def.Launchers) == 0 {
		return v.fieldErr("launchers", "must name at least one launcher")
	}
	for name := range def.Ops {
		if _, ok := opInputs[name]; !ok {
			return v.fieldErr("ops."+name, "unknown operation")
		}
	}
	for _, name := range sortedKeys(def.Launchers) {
		if err := v.launcher(def, name, def.Launchers[name]); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(def.Ops) {
		if err := v.op(name, name, def.Ops[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateBinary(binary string) error {
	if strings.TrimSpace(binary) == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsFunc(binary, unicode.IsControl) {
		return fmt.Errorf("must not contain control characters")
	}
	if strings.ContainsAny(binary, `/\`) && !filepath.IsAbs(binary) {
		return fmt.Errorf("must be a PATH command name or an absolute path")
	}
	return nil
}

type validator struct {
	source    string
	opLabel   string
	stepLabel string
}

func (v validator) fieldErr(field, format string, args ...any) error {
	return &DefinitionError{Source: v.source, Op: v.opLabel, Step: v.stepLabel, Field: field, Detail: fmt.Sprintf(format, args...)}
}

func (v validator) inOp(op string) validator {
	v.opLabel, v.stepLabel = op, ""
	return v
}

func (v validator) atStep(label string) validator {
	v.stepLabel = label
	return v
}

// scope is the set of names a template or condition may reference.
type scope map[string]bool

func newScope(names ...string) scope {
	s := scope{}
	for _, name := range names {
		s[name] = true
	}
	return s
}

func (s scope) with(names ...string) scope {
	out := make(scope, len(s)+len(names))
	for name := range s {
		out[name] = true
	}
	for _, name := range names {
		out[name] = true
	}
	return out
}

func (s scope) allows(name string) bool {
	if env, ok := strings.CutPrefix(name, "env."); ok {
		return envNamePattern.MatchString(env)
	}
	return s[name]
}

func (s scope) withStore(store string, fields []string) scope {
	names := make([]string, 0, len(implicitStoreFields)+len(fields))
	for _, field := range append(append([]string{}, implicitStoreFields...), fields...) {
		names = append(names, "step."+store+"."+field)
	}
	return s.with(names...)
}

func (v validator) template(field, template string, names scope, kind process.TemplateKind) error {
	placeholders, err := process.TemplatePlaceholders(template)
	if err != nil {
		return v.fieldErr(field, "%s", err.Error())
	}
	for _, name := range placeholders {
		if !names.allows(name) {
			return v.fieldErr(field, "unknown placeholder {%s}", name)
		}
	}
	if err := process.ValidateTemplate(template, placeholders, kind); err != nil {
		return v.fieldErr(field, "%s", err.Error())
	}
	return nil
}

func (v validator) argv(field string, argv []string, names scope) error {
	for index, element := range argv {
		label := field + "[" + strconv.Itoa(index) + "]"
		if element == "" {
			return v.fieldErr(label, "empty argv element")
		}
		if err := v.template(label, element, names, process.TemplateArgvTab); err != nil {
			return err
		}
	}
	return nil
}

func (v validator) message(field string, message *Message, names scope) error {
	forms := 0
	if message.Template != "" {
		forms++
	}
	if message.ID != "" {
		forms++
	}
	if message.Or != nil {
		forms++
	}
	if forms != 1 {
		return v.fieldErr(field, "a message is a non-empty template, an id with args, or an or list")
	}
	switch {
	case message.Template != "":
		return v.template(field, message.Template, names, process.TemplateText)
	case message.ID != "":
		if !i18n.Has(message.ID) {
			return v.fieldErr(field, "unknown message id %q", message.ID)
		}
		for index := range message.Args {
			if err := v.message(field+".args["+strconv.Itoa(index)+"]", &message.Args[index], names); err != nil {
				return err
			}
		}
	default:
		if len(message.Or) == 0 {
			return v.fieldErr(field, "or needs at least one message")
		}
		for index := range message.Or {
			if err := v.message(field+".or["+strconv.Itoa(index)+"]", &message.Or[index], names); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v validator) optionalMessage(field string, message *Message, names scope) error {
	if message == nil {
		return nil
	}
	return v.message(field, message, names)
}

func (v validator) conditions(field string, set Conditions, names scope) error {
	for groupIndex, group := range set {
		if len(group) == 0 {
			return v.fieldErr(field, "condition group %d is empty", groupIndex)
		}
		for _, condition := range group {
			if err := v.condition(field, condition, names); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v validator) condition(field, condition string, names scope) error {
	body := strings.TrimPrefix(condition, "!")
	switch {
	case body == "prev_ok" || body == "prev_failed":
		return nil
	case strings.HasPrefix(body, "field_missing:"):
		name := strings.TrimPrefix(body, "field_missing:")
		if !names.allows(name) {
			return v.fieldErr(field, "condition %q references unknown name %q", condition, name)
		}
		return nil
	case strings.HasPrefix(body, "field:"):
		name, value, ok := strings.Cut(strings.TrimPrefix(body, "field:"), "=")
		if !ok {
			return v.fieldErr(field, "condition %q must be field:<name>=<value>", condition)
		}
		if !names.allows(name) {
			return v.fieldErr(field, "condition %q references unknown name %q", condition, name)
		}
		return v.template(field, value, names, process.TemplateArgvTab)
	default:
		return v.fieldErr(field, "unknown condition %q (prev_ok, prev_failed, field:<name>=<value>, field_missing:<name>, optionally prefixed with !)", condition)
	}
}

func (v validator) address(fields []AddressField) error {
	if len(fields) == 0 || len(fields) > 3 {
		return v.fieldErr("address", "must list one to three fields")
	}
	seen := map[string]bool{}
	for index, field := range fields {
		label := "address[" + strconv.Itoa(index) + "]"
		switch field.Name {
		case "session", "container", "pane":
		default:
			return v.fieldErr(label+".name", "must be session, container or pane")
		}
		if seen[field.Name] {
			return v.fieldErr(label+".name", "duplicate field %q", field.Name)
		}
		seen[field.Name] = true
		if field.Pattern != "" {
			re, err := regexp.Compile(field.Pattern)
			if err != nil {
				return v.fieldErr(label+".pattern", "does not compile: %s", err.Error())
			}
			if re.NumSubexp() != 0 {
				return v.fieldErr(label+".pattern", "must not contain capturing groups")
			}
		}
	}
	if !seen["container"] || !seen["pane"] {
		return v.fieldErr("address", "must include container and pane")
	}
	return nil
}

func (v validator) errorRules(rules ErrorRules) error {
	for class, list := range map[string][]string{"errors.gone": rules.Gone, "errors.meta_missing": rules.MetaMissing} {
		for index, rule := range list {
			if _, err := parseErrorRule(rule); err != nil {
				return v.fieldErr(class+"["+strconv.Itoa(index)+"]", "%s", err.Error())
			}
		}
	}
	return nil
}

func (v validator) hooks(def *Definition) error {
	for point, name := range def.Hooks {
		switch point {
		case HookPointReportSession, HookPointFocusPane:
		default:
			return v.fieldErr("hooks."+point, "unknown mount point (report_session, focus_pane)")
		}
		if _, ok := LookupHook(name); !ok {
			return v.fieldErr("hooks."+point, "hook %q is not registered", name)
		}
	}
	if def.Capabilities.SessionReport != (def.Hooks[HookPointReportSession] != "") {
		return v.fieldErr("hooks.report_session", "is required exactly when capabilities.session_report is true")
	}
	if def.Hooks[HookPointFocusPane] != "" && !def.Capabilities.Focus {
		return v.fieldErr("hooks.focus_pane", "requires capabilities.focus")
	}
	return nil
}

func (v validator) launcher(def *Definition, name string, launcher LauncherDefinition) error {
	field := "launchers." + name
	if !definitionNamePattern.MatchString(name) {
		return v.fieldErr(field, "launcher name must match %s", definitionNamePattern)
	}
	switch launcher.Requires.Platform {
	case "posix", "windows", "any":
	default:
		return v.fieldErr(field+".requires.platform", "must be posix, windows or any")
	}
	if launcher.AutoPriority < 0 {
		return v.fieldErr(field+".auto_priority", "must not be negative")
	}
	if launcher.AutoPriority > 0 && !launcher.Requires.InsideSession {
		return v.fieldErr(field+".auto_priority", "auto resolution needs requires.inside_session")
	}
	messageNames := newScope("launcher", "binary", "platform", "name")
	for index, env := range launcher.Requires.Env {
		label := field + ".requires.env[" + strconv.Itoa(index) + "]"
		if !envNamePattern.MatchString(env.Name) {
			return v.fieldErr(label+".name", "invalid environment variable name")
		}
		if err := v.optionalMessage(label+".message", env.Message, messageNames); err != nil {
			return err
		}
	}
	messages := launcher.Requires.Messages
	for label, message := range map[string]*Message{"platform": messages.Platform, "binary": messages.Binary, "env": messages.Env} {
		if err := v.optionalMessage(field+".requires.messages."+label, message, messageNames); err != nil {
			return err
		}
	}
	lines := newScope(startedLineNames...)
	for index := range launcher.StartedLines {
		line := &launcher.StartedLines[index]
		label := field + ".started_lines[" + strconv.Itoa(index) + "]"
		if err := v.conditions(label+".when", line.When, lines); err != nil {
			return err
		}
		if err := v.message(label+".message", &line.Message, lines); err != nil {
			return err
		}
	}
	for op := range launcher.Ops {
		if _, ok := opInputs[op]; !ok {
			return v.fieldErr(field+".ops."+op, "unknown operation")
		}
	}
	for _, op := range sortedKeys(launcher.Ops) {
		if err := v.op(field+".ops."+op, op, launcher.Ops[op]); err != nil {
			return err
		}
	}
	return v.requiredOps(def, name, launcher)
}

func (v validator) requiredOps(def *Definition, name string, launcher LauncherDefinition) error {
	has := func(op string) bool {
		if _, ok := launcher.Ops[op]; ok {
			return true
		}
		_, ok := def.Ops[op]
		return ok
	}
	required := []string{OpCreateContainer, OpRunCommand, OpPaneFacts, OpReadOutput, OpDeliverText, OpTopology, OpContainerExists, OpReverseLookup, OpCloseContainer}
	conditional := map[string]bool{
		OpSetSessionMarker: def.Capabilities.PaneMetadata,
		OpWaitOutput:       def.Capabilities.WaitOutput,
		OpFocus:            def.Capabilities.Focus,
	}
	for op, needed := range conditional {
		if needed {
			required = append(required, op)
		} else if has(op) {
			return v.fieldErr("launchers."+name+".ops", "operation %s is declared but its capability is false", op)
		}
	}
	sort.Strings(required)
	for _, op := range required {
		if !has(op) {
			return v.fieldErr("launchers."+name+".ops", "required operation %s is missing", op)
		}
	}
	return nil
}

// op validates one operation; label names it in errors, including the
// launcher for an override.
func (v validator) op(label, name string, op Op) error {
	v = v.inOp(label)
	if len(op.Steps) == 0 && op.Candidates == nil {
		return v.fieldErr("steps", "must not be empty")
	}
	names := newScope(opInputs[name]...)
	results := newScope(opResults[name]...)
	for key := range op.Result {
		if !results[key] || name == OpReverseLookup {
			return v.fieldErr("result."+key, "is not a result field of %s", name)
		}
	}
	if (op.Unavailable != nil || op.ClosedWhen != nil) && name != OpFocus {
		return v.fieldErr("unavailable", "only the focus operation declares unavailable and closed_when")
	}
	for index := range op.Unavailable {
		line := &op.Unavailable[index]
		label := "unavailable[" + strconv.Itoa(index) + "]"
		if err := v.conditions(label+".when", line.When, names); err != nil {
			return err
		}
		if err := v.message(label+".message", &line.Message, names); err != nil {
			return err
		}
	}
	if err := v.conditions("closed_when", op.ClosedWhen, names.with(paneFactNames...)); err != nil {
		return err
	}
	names, err := v.steps("steps", name, op.Steps, names)
	if err != nil {
		return err
	}
	if op.Candidates != nil {
		if name != OpPrepare && name != OpCreateContainer {
			return v.fieldErr("candidates", "only prepare and create_container declare candidates")
		}
		if names, err = v.candidates(name, op.Candidates, names, results); err != nil {
			return err
		}
	}
	if (op.Rows == nil && name == OpReverseLookup) || (op.Rows != nil && rowResults[name] == nil) {
		return v.fieldErr("rows", "reverse_lookup requires rows, and only reverse_lookup and topology declare them")
	}
	if op.Rows != nil {
		if err := v.rows(name, op.Rows, names); err != nil {
			return err
		}
	}
	for _, key := range sortedKeys(op.Result) {
		if err := v.template("result."+key, op.Result[key], names, process.TemplateText); err != nil {
			return err
		}
	}
	if name == OpCreateContainer && (op.Result["container"] == "" || op.Result["pane"] == "") {
		return v.fieldErr("result", "create_container must map container and pane")
	}
	return nil
}

// steps validates a step list in order and returns the scope extended with
// the stores the steps define.
func (v validator) steps(field, op string, steps []Step, names scope) (scope, error) {
	for index := range steps {
		sv := v.atStep(field + "[" + strconv.Itoa(index) + "]")
		next, err := sv.step(op, &steps[index], names)
		if err != nil {
			return nil, err
		}
		names = next
	}
	return names, nil
}

func (v validator) step(op string, step *Step, names scope) (scope, error) {
	if err := v.conditions("when", step.When, names); err != nil {
		return nil, err
	}
	if (step.Fail != nil) == (step.Argv != nil) {
		return nil, v.fieldErr("argv", "a step declares exactly one of argv and fail")
	}
	if step.Fail != nil {
		if step.Store != "" || step.Output != nil || step.Fields != nil || step.Expect != nil || step.Poll != nil ||
			step.OnError != "" || step.Timeout != "" || step.StopOnSuccess || step.Messages != (StepMessages{}) {
			return nil, v.fieldErr("fail", "a fail step declares only when and fail")
		}
		return names, v.message("fail", step.Fail, names)
	}
	if len(step.Argv) == 0 {
		return nil, v.fieldErr("argv", "must not be empty")
	}
	if err := v.argv("argv", step.Argv, names); err != nil {
		return nil, err
	}
	if step.Output != nil {
		if err := process.ValidateTerminalOutput(*step.Output); err != nil {
			return nil, v.fieldErr("output", "%s", err.Error())
		}
		if step.Output.Format != "" && step.Output.Format != process.FormatJSON {
			return nil, v.fieldErr("output.format", "terminal definitions use the whole-document form only")
		}
	}
	switch step.OnError {
	case "", OnErrorFail, OnErrorContinue, OnErrorMetaMissing:
	default:
		return nil, v.fieldErr("on_error", "must be fail, continue or meta_missing")
	}
	if step.Timeout != "" {
		if _, err := positiveDuration(step.Timeout); err != nil {
			return nil, v.fieldErr("timeout", "%s", err.Error())
		}
	}
	if err := v.poll(op, step.Poll, names); err != nil {
		return nil, err
	}
	messageNames := names.with(stepMessageNames...)
	for label, message := range map[string]*Message{
		"exec": step.Messages.Exec, "exit": step.Messages.Exit, "invalid": step.Messages.Invalid,
		"not_json": step.Messages.NotJSON, "not_object": step.Messages.NotObject, "missing_result": step.Messages.MissingResult,
		"gone": step.Messages.Gone,
	} {
		if err := v.optionalMessage("messages."+label, message, messageNames); err != nil {
			return nil, err
		}
	}
	if (step.Fields != nil || step.Expect != nil || step.GoneWhen != nil) && step.Store == "" {
		return nil, v.fieldErr("store", "fields, expect and gone_when need a store name")
	}
	if step.GoneWhen != nil && op != OpPaneFacts && op != OpContainerExists {
		return nil, v.fieldErr("gone_when", "only pane_facts and container_exists steps declare gone_when")
	}
	if step.Messages.Gone != nil && step.GoneWhen == nil {
		return nil, v.fieldErr("messages.gone", "is only used with gone_when")
	}
	if step.Store == "" {
		return names, nil
	}
	if !storeNamePattern.MatchString(step.Store) {
		return nil, v.fieldErr("store", "must match %s", storeNamePattern)
	}
	fields, err := v.fields("fields", step.Fields)
	if err != nil {
		return nil, err
	}
	names = names.withStore(step.Store, fields)
	if err := v.conditions("expect", step.Expect, names); err != nil {
		return nil, err
	}
	if err := v.conditions("gone_when", step.GoneWhen, names); err != nil {
		return nil, err
	}
	return names, nil
}

func (v validator) fields(field string, fields map[string]string) ([]string, error) {
	names := sortedKeys(fields)
	for _, name := range names {
		if !storeNamePattern.MatchString(name) {
			return nil, v.fieldErr(field+"."+name, "field name must match %s", storeNamePattern)
		}
		for _, implicit := range implicitStoreFields {
			if name == implicit {
				return nil, v.fieldErr(field+"."+name, "field name is reserved")
			}
		}
		spec := process.OutputSpec{Source: process.SourceStdout, Parse: fields[name]}
		if err := process.ValidateTerminalOutput(spec); err != nil {
			return nil, v.fieldErr(field+"."+name, "%s", err.Error())
		}
	}
	return names, nil
}

func (v validator) poll(op string, poll *Poll, names scope) error {
	if poll == nil {
		return nil
	}
	if _, err := positiveDuration(poll.Interval); err != nil {
		return v.fieldErr("poll.interval", "%s", err.Error())
	}
	if poll.Timeout == "{timeout_ms}" {
		if !names.allows("timeout_ms") {
			return v.fieldErr("poll.timeout", "{timeout_ms} is only available to wait_output")
		}
	} else if _, err := positiveDuration(poll.Timeout); err != nil {
		return v.fieldErr("poll.timeout", "%s", err.Error())
	}
	switch {
	case poll.Until == "matched":
		if op != OpWaitOutput {
			return v.fieldErr("poll.until", "matched compares the wait_output marker and is only valid there")
		}
	case poll.Until == "nonempty":
	case strings.HasPrefix(poll.Until, "json_field:"):
		path, _, ok := strings.Cut(strings.TrimPrefix(poll.Until, "json_field:"), "=")
		spec := process.OutputSpec{Source: process.SourceStdout, Parse: "json_field:" + path}
		if !ok || process.ValidateTerminalOutput(spec) != nil {
			return v.fieldErr("poll.until", "must be json_field:<dotted.path>=<value>")
		}
	default:
		return v.fieldErr("poll.until", "must be matched, nonempty or json_field:<dotted.path>=<value>")
	}
	return nil
}

func (v validator) candidates(op string, candidates *Candidates, names scope, results scope) (scope, error) {
	if len(candidates.Values) == 0 {
		return nil, v.fieldErr("candidates.values", "must not be empty")
	}
	for index, value := range candidates.Values {
		if err := v.template("candidates.values["+strconv.Itoa(index)+"]", value, names, process.TemplateArgvTab); err != nil {
			return nil, err
		}
	}
	if len(candidates.Steps) == 0 {
		return nil, v.fieldErr("candidates.steps", "must not be empty")
	}
	candidateNames, err := v.steps("candidates.steps", op, candidates.Steps, names.with("candidate"))
	if err != nil {
		return nil, err
	}
	if len(candidates.Select) == 0 {
		return nil, v.fieldErr("candidates.select", "must not be empty")
	}
	for index, entry := range candidates.Select {
		label := "candidates.select[" + strconv.Itoa(index) + "]"
		if len(entry.When) == 0 {
			return nil, v.fieldErr(label+".when", "must not be empty")
		}
		if err := v.conditions(label+".when", entry.When, candidateNames); err != nil {
			return nil, err
		}
		for _, key := range sortedKeys(entry.Result) {
			if !results[key] {
				return nil, v.fieldErr(label+".result."+key, "is not a result field of %s", op)
			}
			if err := v.template(label+".result."+key, entry.Result[key], candidateNames, process.TemplateText); err != nil {
				return nil, err
			}
		}
	}
	// Candidate stores are discarded when no candidate is picked, so the
	// fallback sees only the stores of the operation steps.
	return v.steps("candidates.fallback", op, candidates.Fallback, names)
}

func (v validator) rows(op string, rows *Rows, names scope) error {
	if !names["step."+rows.From+".text"] {
		return v.fieldErr("rows.from", "must name a store of an earlier step")
	}
	if rows.Split != "" && rows.Split != "lines" {
		path, ok := strings.CutPrefix(rows.Split, "json_array:")
		spec := process.OutputSpec{Source: process.SourceStdout, Parse: "json_field:" + path}
		if !ok || process.ValidateTerminalOutput(spec) != nil {
			return v.fieldErr("rows.split", "must be lines or json_array:<dotted.path>")
		}
	}
	if len(rows.Fields) == 0 {
		return v.fieldErr("rows.fields", "must not be empty")
	}
	fields, err := v.fields("rows.fields", rows.Fields)
	if err != nil {
		return err
	}
	rowNames := names
	for _, field := range fields {
		rowNames = rowNames.with("row." + field)
	}
	if err := v.conditions("rows.expect", rows.Expect, rowNames); err != nil {
		return err
	}
	for index := range rows.Checks {
		check := &rows.Checks[index]
		label := "rows.checks[" + strconv.Itoa(index) + "]"
		if len(check.When) == 0 {
			return v.fieldErr(label+".when", "must not be empty")
		}
		if err := v.conditions(label+".when", check.When, rowNames); err != nil {
			return err
		}
		if err := v.message(label+".message", &check.Message, rowNames); err != nil {
			return err
		}
	}
	if len(rows.Match) == 0 {
		return v.fieldErr("rows.match", "must not be empty")
	}
	if err := v.conditions("rows.match", rows.Match, rowNames); err != nil {
		return err
	}
	results := newScope(rowResults[op]...)
	for _, key := range sortedKeys(rows.Result) {
		if !results[key] {
			return v.fieldErr("rows.result."+key, "is not a row result field of %s", op)
		}
		if err := v.template("rows.result."+key, rows.Result[key], rowNames, process.TemplateText); err != nil {
			return err
		}
	}
	if rows.Result["pane"] == "" || (op == OpReverseLookup && rows.Result["container"] == "") {
		return v.fieldErr("rows.result", "must map the pane (and the container for reverse_lookup)")
	}
	if err := v.optionalMessage("rows.messages.missing", rows.Messages.Missing, names); err != nil {
		return err
	}
	if err := v.optionalMessage("rows.messages.invalid", rows.Messages.Invalid, names); err != nil {
		return err
	}
	if err := v.optionalMessage("rows.messages.none", rows.Messages.None, names); err != nil {
		return err
	}
	return v.optionalMessage("rows.messages.ambiguous", rows.Messages.Ambiguous, names.with("count"))
}

func positiveDuration(text string) (time.Duration, error) {
	duration, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", text)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", text)
	}
	return duration, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
