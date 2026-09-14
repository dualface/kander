package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dualface/kander/internal/process"
)

// DefinitionSchemaVersion is the only terminal definition schema this binary
// reads. It changes only on a breaking change; optional fields do not bump it.
const DefinitionSchemaVersion = 1

// Operation names of a definition; each maps to one Backend method.
const (
	OpPrepare          = "prepare"
	OpCreateContainer  = "create_container"
	OpWaitReady        = "wait_ready"
	OpRunCommand       = "run_command"
	OpSetSessionMarker = "set_session_marker"
	OpPaneFacts        = "pane_facts"
	OpReadOutput       = "read_output"
	OpWaitOutput       = "wait_output"
	OpDeliverText      = "deliver_text"
	OpTopology         = "topology"
	OpContainerExists  = "container_exists"
	OpReverseLookup    = "reverse_lookup"
	OpFocus            = "focus"
	OpCloseContainer   = "close_container"
)

// Hook mount points a definition may bind to a registered hook.
const (
	HookPointReportSession = "report_session"
	// HookPointFocusPane runs after the focus steps switched the container,
	// to focus the pane itself.
	HookPointFocusPane = "focus_pane"
)

// Step failure policies.
const (
	OnErrorFail        = "fail"
	OnErrorContinue    = "continue"
	OnErrorMetaMissing = "meta_missing"
)

// Definition is one declarative terminal definition file. The format is
// documented in docs/terminal-definitions.md.
type Definition struct {
	SchemaVersion int                           `json:"schema_version"`
	Name          string                        `json:"name"`
	Binary        string                        `json:"binary"`
	VersionArgs   []string                      `json:"version_args,omitempty"`
	Capabilities  DefinitionCapabilities        `json:"capabilities"`
	Address       []AddressField                `json:"address"`
	Launchers     map[string]LauncherDefinition `json:"launchers"`
	Errors        ErrorRules                    `json:"errors"`
	Hooks         map[string]string             `json:"hooks,omitempty"`
	Ops           map[string]Op                 `json:"ops"`
}

// DefinitionCapabilities are the capability flags a definition declares.
// POSIXOnly follows each launcher's requires.platform.
type DefinitionCapabilities struct {
	Container         bool `json:"container"`
	Focus             bool `json:"focus"`
	PaneMetadata      bool `json:"pane_metadata"`
	ForegroundProcess bool `json:"foreground_process"`
	WaitOutput        bool `json:"wait_output"`
	SessionReport     bool `json:"session_report"`
	AgentIdentity     bool `json:"agent_identity"`
}

// AddressField is one colon-separated field of the opaque WINDOW part.
type AddressField struct {
	// Name is session, container or pane.
	Name string `json:"name"`
	// Pattern is a regular expression without capturing groups; the default
	// is one colon-free, whitespace-free segment.
	Pattern string `json:"pattern,omitempty"`
}

// LauncherDefinition is one launcher name the definition provides.
type LauncherDefinition struct {
	// AutoPriority > 0 lets auto resolution select this launcher; higher wins.
	AutoPriority int      `json:"auto_priority,omitempty"`
	Requires     Requires `json:"requires"`
	// StartedLines render the launch report after the head line.
	StartedLines []MessageLine `json:"started_lines,omitempty"`
	// Ops replace whole operations of the definition for this launcher.
	Ops map[string]Op `json:"ops,omitempty"`
}

// Requires are the launch preconditions checked before any container exists.
type Requires struct {
	// Platform is posix, windows or any.
	Platform string `json:"platform"`
	// Binary requires the definition binary on PATH.
	Binary bool `json:"binary"`
	// InsideSession means the launcher runs inside an existing session of
	// the terminal; only such launchers take part in auto resolution.
	InsideSession bool             `json:"inside_session"`
	Env           []EnvRequirement `json:"env,omitempty"`
	Messages      RequireMessages  `json:"messages,omitempty"`
}

// EnvRequirement requires a non-empty environment variable, optionally with
// an exact value.
type EnvRequirement struct {
	Name  string  `json:"name"`
	Value *string `json:"value,omitempty"`
	// Trim compares the value without surrounding whitespace, and
	// {env.<NAME>} expands to the trimmed value.
	Trim bool `json:"trim,omitempty"`
	// BeforeBinary checks this variable before the binary lookup.
	BeforeBinary bool `json:"before_binary,omitempty"`
	// SkipAuto leaves this variable out of auto detection; Prepare still
	// requires it, so auto selects the launcher and reports what is missing.
	SkipAuto bool `json:"skip_auto,omitempty"`
	// Message replaces requires.messages.env for this variable.
	Message *Message `json:"message,omitempty"`
}

// RequireMessages override the generic precondition diagnostics.
type RequireMessages struct {
	Platform *Message `json:"platform,omitempty"`
	Binary   *Message `json:"binary,omitempty"`
	Env      *Message `json:"env,omitempty"`
}

// ErrorRules classify a non-zero command exit. Rules are exit:<code>,
// stderr:<regex>, stdout_json:<dotted.path>=<value> or
// stderr_json:<dotted.path>=<value>; any matching rule classifies.
type ErrorRules struct {
	Gone        []string `json:"gone,omitempty"`
	MetaMissing []string `json:"meta_missing,omitempty"`
}

// Op is one operation: ordered steps, then optional candidates or rows, then
// the result mapping.
type Op struct {
	Steps      []Step            `json:"steps,omitempty"`
	Candidates *Candidates       `json:"candidates,omitempty"`
	Rows       *Rows             `json:"rows,omitempty"`
	Result     map[string]string `json:"result,omitempty"`
	// Unavailable (focus only) ends focus before any command when a
	// condition holds.
	Unavailable []MessageLine `json:"unavailable,omitempty"`
	// ClosedWhen (focus only) treats the probed pane as closed.
	ClosedWhen Conditions `json:"closed_when,omitempty"`
}

// Step runs one argv command, or fails the operation with a message.
type Step struct {
	Store  string              `json:"store,omitempty"`
	When   Conditions          `json:"when,omitempty"`
	Argv   []string            `json:"argv,omitempty"`
	Fail   *Message            `json:"fail,omitempty"`
	Output *process.OutputSpec `json:"output,omitempty"`
	Fields map[string]string   `json:"fields,omitempty"`
	Expect Conditions          `json:"expect,omitempty"`
	// GoneWhen (pane_facts and container_exists only) reports the target as
	// gone when a successful command's output satisfies it, for terminals
	// that answer a closed target with exit 0 and empty fields.
	GoneWhen      Conditions   `json:"gone_when,omitempty"`
	Poll          *Poll        `json:"poll,omitempty"`
	OnError       string       `json:"on_error,omitempty"`
	Timeout       string       `json:"timeout,omitempty"`
	StopOnSuccess bool         `json:"stop_on_success,omitempty"`
	Messages      StepMessages `json:"messages,omitempty"`
}

// StepMessages render a failed step: exec when the command could not run,
// exit on a non-zero exit, invalid when the output does not parse or satisfy
// expect. not_json, not_object and missing_result refine invalid for a
// json_field output and fall back to it.
type StepMessages struct {
	Exec          *Message `json:"exec,omitempty"`
	Exit          *Message `json:"exit,omitempty"`
	Invalid       *Message `json:"invalid,omitempty"`
	NotJSON       *Message `json:"not_json,omitempty"`
	NotObject     *Message `json:"not_object,omitempty"`
	MissingResult *Message `json:"missing_result,omitempty"`
	// Gone is the gone detail when gone_when holds.
	Gone *Message `json:"gone,omitempty"`
}

// forKind returns the message of an output failure kind.
func (m StepMessages) forKind(kind ErrorKind) *Message {
	refined := map[ErrorKind]*Message{KindNotJSON: m.NotJSON, KindNotObject: m.NotObject, KindMissingResult: m.MissingResult}[kind]
	if refined != nil {
		return refined
	}
	return m.Invalid
}

// Poll reruns a step until its output satisfies Until or Timeout elapses.
type Poll struct {
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
	// Until is matched, nonempty or json_field:<dotted.path>=<value>.
	Until string `json:"until"`
}

// Candidates try values in order: each value runs the steps, and the first
// select entry whose condition holds picks it. Without a pick the fallback
// steps run.
type Candidates struct {
	Values   []string          `json:"values"`
	Steps    []Step            `json:"steps"`
	Select   []CandidateSelect `json:"select"`
	Fallback []Step            `json:"fallback,omitempty"`
}

// CandidateSelect picks the current candidate and sets result fields.
type CandidateSelect struct {
	When   Conditions        `json:"when"`
	Result map[string]string `json:"result"`
}

// Rows scan one stored step output row by row: lines, or the elements of a
// JSON array (reverse_lookup and topology only).
type Rows struct {
	From string `json:"from"`
	// Split is lines (default) or json_array:<dotted.path>; each array
	// element becomes one row re-encoded as compact JSON with sorted keys.
	Split  string            `json:"split,omitempty"`
	Fields map[string]string `json:"fields"`
	Expect Conditions        `json:"expect,omitempty"`
	// Checks run after expect, in order; the first that does not hold fails
	// the operation with its own message.
	Checks   []RowCheck        `json:"checks,omitempty"`
	Match    Conditions        `json:"match"`
	Result   map[string]string `json:"result"`
	Messages RowMessages       `json:"messages"`
}

// RowCheck is one ordered row validation with its own message.
type RowCheck struct {
	When    Conditions `json:"when"`
	Message Message    `json:"message"`
}

// RowMessages render a missing JSON array, an invalid row, no match, and
// several matches.
type RowMessages struct {
	Missing   *Message `json:"missing,omitempty"`
	Invalid   *Message `json:"invalid,omitempty"`
	None      *Message `json:"none,omitempty"`
	Ambiguous *Message `json:"ambiguous,omitempty"`
}

// MessageLine is a message rendered only when its condition holds.
type MessageLine struct {
	When    Conditions `json:"when,omitempty"`
	Message Message    `json:"message"`
}

// Conditions are alternatives of conjunctions: the set holds when every
// condition of any one inner list holds. JSON accepts one condition string
// or an array of string arrays. An empty set always holds.
type Conditions [][]string

func (c *Conditions) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*c = Conditions{{single}}
		return nil
	}
	var groups [][]string
	if err := json.Unmarshal(data, &groups); err != nil {
		return fmt.Errorf("conditions must be a string or an array of string arrays")
	}
	*c = groups
	return nil
}

// Message is a diagnostic: a text template, a catalog message with argument
// messages, or the first non-empty of several messages.
type Message struct {
	Template string
	ID       string
	Args     []Message
	Or       []Message
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var template string
	if err := json.Unmarshal(data, &template); err == nil {
		*m = Message{Template: template}
		return nil
	}
	var object struct {
		ID   string    `json:"id"`
		Args []Message `json:"args"`
		Or   []Message `json:"or"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&object); err != nil {
		return fmt.Errorf("message must be a template string, {\"id\", \"args\"} or {\"or\"}: %w", err)
	}
	*m = Message{ID: object.ID, Args: object.Args, Or: object.Or}
	return nil
}

// DecodeDefinition decodes a definition file, rejecting unknown fields and
// trailing data, then validates it. source names the file in errors.
func DecodeDefinition(source string, data []byte) (*Definition, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var def Definition
	if err := decoder.Decode(&def); err != nil {
		return nil, &DefinitionError{Source: source, Detail: err.Error()}
	}
	if decoder.More() {
		return nil, &DefinitionError{Source: source, Detail: "trailing data after the definition object"}
	}
	if err := ValidateDefinition(source, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// DefinitionError is a definition validation failure. Its text names the
// file, the operation, the step index and the field.
type DefinitionError struct {
	Source string
	Op     string
	Step   string
	Field  string
	Detail string
}

func (e *DefinitionError) Error() string {
	parts := []string{e.Source}
	if e.Op != "" {
		parts = append(parts, "op "+e.Op)
	}
	if e.Step != "" {
		parts = append(parts, "step "+e.Step)
	}
	if e.Field != "" {
		parts = append(parts, "field "+e.Field)
	}
	return strings.Join(parts, ": ") + ": " + e.Detail
}
