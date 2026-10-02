package board

import (
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// Optional card fields that pin the execution and review agents, models and
// efforts of one card regardless of the configuration. They are read only from
// the card header (the text before the first `## ` heading), so a line with the
// same spelling in a body section never takes effect. Kander never writes the
// user fields; it writes only the *_RESOLVED records.
const (
	FieldExecAgent    = "EXEC_AGENT"
	FieldExecModel    = "EXEC_MODEL"
	FieldExecEffort   = "EXEC_EFFORT"
	FieldExecResolved = "EXEC_RESOLVED"
)

// PinRoles are the review roles that accept REVIEW_<ROLE>_* fields.
var PinRoles = []string{"PMQA", "Security"}

// Pin item suffixes shared by the execution and review fields.
const (
	pinAgent    = "AGENT"
	pinModel    = "MODEL"
	pinEffort   = "EFFORT"
	pinResolved = "RESOLVED"
)

// pinValueRe limits model and effort values to one ASCII token that cannot be
// read as an option, because the value becomes an argv element that terminal
// launchers render into a shell command line.
var pinValueRe = regexp.MustCompile(`^[A-Za-z0-9._:/@+\[\]_][A-Za-z0-9._:/@+\[\]_-]*$`)

// StagePin is the forced agent, model and effort of one stage; an empty value
// leaves that item to the configuration.
type StagePin struct {
	Agent  string
	Model  string
	Effort string
}

// Forced reports whether any item of the stage is pinned.
func (p StagePin) Forced() bool { return p.Agent != "" || p.Model != "" || p.Effort != "" }

// CardPins holds every pin of one card; Review is keyed by the role names in PinRoles.
type CardPins struct {
	Exec   StagePin
	Review map[string]StagePin
}

// Any reports whether the card pins anything, which also decides whether Kander
// writes *_RESOLVED records on it.
func (c CardPins) Any() bool {
	if c.Exec.Forced() {
		return true
	}
	for _, pin := range c.Review {
		if pin.Forced() {
			return true
		}
	}
	return false
}

// ReviewPinField returns the REVIEW_<ROLE>_<ITEM> field name of a role.
func ReviewPinField(role, item string) string {
	return "REVIEW_" + strings.ToUpper(role) + "_" + item
}

// ReviewResolvedField returns the REVIEW_<ROLE>_RESOLVED record name of a role.
func ReviewResolvedField(role string) string { return ReviewPinField(role, pinResolved) }

func execPinFields() [3]string { return [3]string{FieldExecAgent, FieldExecModel, FieldExecEffort} }

func reviewPinFields(role string) [3]string {
	return [3]string{ReviewPinField(role, pinAgent), ReviewPinField(role, pinModel), ReviewPinField(role, pinEffort)}
}

// pinUserFields lists every user-owned pin field.
func pinUserFields() []string {
	fields := execPinFields()
	out := fields[:]
	for _, role := range PinRoles {
		review := reviewPinFields(role)
		out = append(out, review[:]...)
	}
	return out
}

// pinResolvedFields lists every Kander-owned resolution record.
func pinResolvedFields() []string {
	out := []string{FieldExecResolved}
	for _, role := range PinRoles {
		out = append(out, ReviewResolvedField(role))
	}
	return out
}

// CardHeader returns the text before the first `## ` heading.
func CardHeader(text string) string {
	if loc := headingRe.FindStringIndex(text); loc != nil {
		return text[:loc[0]]
	}
	return text
}

// headerFieldRes holds the compiled header regexp of every pin field; it is
// filled once at package init and only read afterwards.
var headerFieldRes = func() map[string]*regexp.Regexp {
	out := map[string]*regexp.Regexp{}
	for _, name := range append(pinUserFields(), pinResolvedFields()...) {
		out[name] = regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(name) + `:[ \t]*(.*?)[ \t\r]*$`)
	}
	return out
}()

func headerFieldRe(name string) *regexp.Regexp { return headerFieldRes[name] }

// headerLines returns the complete header lines of one field.
func headerLines(text, name string) []string {
	return headerFieldRe(name).FindAllString(CardHeader(text), -1)
}

func headerValue(text, name string) (string, int) {
	matches := headerFieldRe(name).FindAllStringSubmatch(CardHeader(text), -1)
	if len(matches) == 0 {
		return "", 0
	}
	return matches[0][1], len(matches)
}

// pinValue normalizes one user field: empty and `auto` both leave the item to the configuration.
func pinValue(raw string) string {
	if strings.EqualFold(raw, "auto") {
		return ""
	}
	return raw
}

// ParseCardPins reads the header pin fields and checks their syntax: no repeated
// field line, agent names that follow the agent naming rule, model and effort
// values that pass pinValueRe, and no model or effort without the stage agent.
// Checks against the loaded agent definitions live in ValidateCardPins.
func ParseCardPins(text string) (CardPins, error) {
	pins := CardPins{Review: map[string]StagePin{}}
	exec, err := parseStagePin(text, execPinFields())
	if err != nil {
		return CardPins{}, err
	}
	pins.Exec = exec
	for _, role := range PinRoles {
		pin, err := parseStagePin(text, reviewPinFields(role))
		if err != nil {
			return CardPins{}, err
		}
		pins.Review[role] = pin
	}
	for _, name := range pinResolvedFields() {
		if len(headerLines(text, name)) > 1 {
			return CardPins{}, kanbanError("board.pin_duplicate_field", name)
		}
	}
	return pins, nil
}

func parseStagePin(text string, fields [3]string) (StagePin, error) {
	var values [3]string
	for i, name := range fields {
		raw, count := headerValue(text, name)
		if count > 1 {
			return StagePin{}, kanbanError("board.pin_duplicate_field", name)
		}
		values[i] = pinValue(raw)
	}
	pin := StagePin{Agent: values[0], Model: values[1], Effort: values[2]}
	if pin.Agent != "" && !config.ValidAgentName(pin.Agent) {
		return StagePin{}, kanbanError("board.pin_invalid_agent", fields[0], pin.Agent)
	}
	for i, value := range []string{pin.Model, pin.Effort} {
		if value != "" && !pinValueRe.MatchString(value) {
			return StagePin{}, kanbanError("board.pin_invalid_value", fields[i+1], value)
		}
	}
	if pin.Agent == "" && (pin.Model != "" || pin.Effort != "") {
		return StagePin{}, kanbanError("board.pin_requires_agent", fields[0])
	}
	return pin, nil
}

// ValidateCardPins checks parsed pins against the loaded agent definitions: the
// execution agent must be a configured execution agent, a review agent must have
// a review template, and an effort is accepted only when the stage's argv
// template consumes {effort}.
func ValidateCardPins(cfg *config.Config, pins CardPins) error {
	if pin := pins.Exec; pin.Agent != "" {
		if !config.HasAgent(cfg, pin.Agent) {
			return kanbanError("board.pin_unknown_agent", FieldExecAgent, pin.Agent)
		}
		if pin.Effort != "" && !ExecutionTakesEffort(cfg, pin.Agent) {
			return kanbanError("board.pin_effort_unsupported", FieldExecEffort, pin.Agent)
		}
	}
	for _, role := range PinRoles {
		pin := pins.Review[role]
		if pin.Agent == "" {
			continue
		}
		if !config.HasReviewTemplate(cfg, pin.Agent) {
			return kanbanError("board.pin_unknown_reviewer", ReviewPinField(role, pinAgent), pin.Agent)
		}
		if pin.Effort != "" && !ReviewTakesEffort(cfg, pin.Agent) {
			return kanbanError("board.pin_effort_unsupported", ReviewPinField(role, pinEffort), pin.Agent)
		}
	}
	return nil
}

// ExecutionTakesEffort reports whether the agent's start argv template consumes {effort}.
func ExecutionTakesEffort(cfg *config.Config, agent string) bool {
	def := config.AgentFor(cfg, agent)
	return def.Args != nil && templateTakesEffort(def.Args.Start)
}

// ReviewTakesEffort reports whether the agent's review argv template consumes {effort}.
func ReviewTakesEffort(cfg *config.Config, agent string) bool {
	def := config.AgentFor(cfg, agent)
	return def.Args != nil && templateTakesEffort(def.Args.Review)
}

func templateTakesEffort(template []string) bool {
	for _, arg := range template {
		if strings.Contains(arg, "{effort}") {
			return true
		}
	}
	return false
}

// CardPinsOf parses and validates the pins of one card.
func CardPinsOf(cfg *config.Config, text string) (CardPins, error) {
	pins, err := ParseCardPins(text)
	if err != nil {
		return CardPins{}, err
	}
	if err := ValidateCardPins(cfg, pins); err != nil {
		return CardPins{}, err
	}
	return pins, nil
}

// cardPinProblem returns the pin defect of a card for check and the todo gate;
// a card without pin fields never yields one.
func cardPinProblem(text string) error {
	if !hasPinField(text) {
		return nil
	}
	cfg, err := config.Effective(nil)
	if err != nil {
		return err
	}
	_, err = CardPinsOf(cfg, text)
	return err
}

func hasPinField(text string) bool {
	header := CardHeader(text)
	for _, name := range append(pinUserFields(), pinResolvedFields()...) {
		if headerFieldRe(name).MatchString(header) {
			return true
		}
	}
	return false
}

// Pin sources recorded in *_RESOLVED.
const (
	PinSourceForced = "forced"
	PinSourceCLI    = "cli"
)

// PinSourceConfig is the source of a value taken from the configuration at one scale.
func PinSourceConfig(scale string) string { return "config:" + scale }

// ResolvedItem is one resolved value with its source. An empty Value renders as
// `-`; NotApplicable renders `N/A` for an effort the agent does not take.
type ResolvedItem struct {
	Value         string
	Source        string
	NotApplicable bool
}

func (r ResolvedItem) render(name string) string {
	if r.NotApplicable {
		return name + "=N/A"
	}
	value := r.Value
	if value == "" {
		value = "-"
	}
	return name + "=" + value + "(" + r.Source + ")"
}

// Resolution is the agent, model and effort one stage actually used.
type Resolution struct {
	Agent  ResolvedItem
	Model  ResolvedItem
	Effort ResolvedItem
}

// Render returns the record value: `agent=<v>(<src>) model=<v>(<src>) effort=<v>(<src>)`.
func (r Resolution) Render() string {
	return r.Agent.render("agent") + " " + r.Model.render("model") + " " + r.Effort.render("effort")
}

// WithResolvedRecord writes one *_RESOLVED header line. An existing line is
// replaced in place; otherwise the line goes after the last header metadata
// line. A card that pins nothing is returned unchanged, so unpinned cards keep
// their exact bytes.
func WithResolvedRecord(text, field, value string) (string, error) {
	pins, err := ParseCardPins(text)
	if err != nil {
		return "", err
	}
	if !pins.Any() {
		return text, nil
	}
	line := RenderField(field, value)
	header := CardHeader(text)
	re := headerFieldRe(field)
	if loc := re.FindStringIndex(header); loc != nil {
		return text[:loc[0]] + line + text[loc[1]:], nil
	}
	insert := lastHeaderMetadataEnd(header)
	if insert < 0 {
		return "", kanbanError("board.pin_header_missing", field)
	}
	return text[:insert] + "\n" + line + text[insert:], nil
}

var headerMetadataLineRe = regexp.MustCompile(`(?m)^- [^\r\n]*`)

// lastHeaderMetadataEnd returns the offset just past the last `- ` line of the header, or -1.
func lastHeaderMetadataEnd(header string) int {
	all := headerMetadataLineRe.FindAllStringIndex(header, -1)
	if len(all) == 0 {
		return -1
	}
	return all[len(all)-1][1]
}

var resolvedLineRes = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, name := range pinResolvedFields() {
		out = append(out, regexp.MustCompile(`(?m)^- `+regexp.QuoteMeta(name)+`:[^\n]*\n?`))
	}
	return out
}()

// WithoutResolvedRecords removes the *_RESOLVED header lines. Review task-context
// snapshots use it so that rewriting a record never changes a frozen batch
// context; text without such lines is returned unchanged.
func WithoutResolvedRecords(text string) string {
	header := CardHeader(text)
	stripped := header
	for _, re := range resolvedLineRes {
		stripped = re.ReplaceAllString(stripped, "")
	}
	if stripped == header {
		return text
	}
	return stripped + text[len(header):]
}

// pinFrozenFields returns the header pin lines that freeze with the contract.
// A card without them contributes nothing, so frozen-contract records of old
// cards keep their exact shape.
func pinFrozenFields(text string) map[string]string {
	out := map[string]string{}
	for _, name := range pinUserFields() {
		if lines := headerLines(text, name); len(lines) > 0 {
			out[name] = strings.Join(lines, "\n")
		}
	}
	return out
}

// pinDuplicateField names the first pin field or record repeated in the header.
func pinDuplicateField(text string) string {
	for _, name := range append(pinUserFields(), pinResolvedFields()...) {
		if len(headerLines(text, name)) > 1 {
			return name
		}
	}
	return ""
}

// pinManagedViolation names the first *_RESOLVED record an ordinary update changed.
func pinManagedViolation(old, next string) string {
	for _, name := range pinResolvedFields() {
		if strings.Join(headerLines(old, name), "\n") != strings.Join(headerLines(next, name), "\n") {
			return name
		}
	}
	return ""
}
