package process

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Output source, format, and the three parse primitives.
const (
	SourceStdout = "stdout"
	SourceStderr = "stderr"
	SourceFile   = "file"

	FormatJSON   = "json"
	FormatNDJSON = "ndjson"

	ParseRaw = "raw"

	parsePrefixJSONField = "json_field:"
	parsePrefixRegex     = "regex:"
)

// ErrSuccess means declared success conditions were not met.
var ErrSuccess = errors.New("success conditions not met")

// SpecError is a validation failure. Error text includes the field name and the illegal value.
type SpecError struct {
	Field string
	Value string
	Msg   string
}

func (e *SpecError) Error() string {
	if e.Value == "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Msg)
	}
	return fmt.Sprintf("%s: %s (%q)", e.Field, e.Msg, e.Value)
}

func specErrorf(field, value, format string, args ...any) *SpecError {
	return &SpecError{Field: field, Value: value, Msg: fmt.Sprintf(format, args...)}
}

// OutputSpec is the declarative output parser shared by review templates and terminal definitions.
// format / select / join describe stream shape; they are not a fourth parse primitive.
type OutputSpec struct {
	Source  string          `json:"source"`
	Format  string          `json:"format,omitempty"`
	Select  []LineCondition `json:"select,omitempty"`
	Parse   string          `json:"parse"`
	Join    *string         `json:"join,omitempty"`
	Success []LineCondition `json:"success,omitempty"`
}

// LineCondition is the shared vocabulary of select and success.
// Exactly one of Equals or Absent=true must be set. Equals uses RawMessage
// so JSON null is distinct from an omitted field.
type LineCondition struct {
	JSONField string          `json:"json_field"`
	Equals    json.RawMessage `json:"equals,omitempty"`
	Absent    *bool           `json:"absent,omitempty"`
}

// DecodeOutputSpec decodes one spec and rejects unknown fields, then validates it.
func DecodeOutputSpec(data []byte) (OutputSpec, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var spec OutputSpec
	if err := dec.Decode(&spec); err != nil {
		return OutputSpec{}, specErrorf("output", "", "%s", err.Error())
	}
	if err := ValidateOutput(spec); err != nil {
		return OutputSpec{}, err
	}
	return spec, nil
}

// ValidateOutput checks the full source set stdout|stderr|file.
func ValidateOutput(spec OutputSpec) error {
	switch spec.Source {
	case SourceStdout, SourceStderr, SourceFile:
	default:
		return specErrorf("source", spec.Source, "must be stdout, stderr, or file")
	}

	format := spec.effectiveFormat()
	switch format {
	case FormatJSON, FormatNDJSON:
	default:
		return specErrorf("format", spec.Format, "must be json or ndjson")
	}

	if format == FormatJSON {
		if spec.Select != nil {
			return specErrorf("select", "present", "only valid when format is ndjson")
		}
		if spec.Join != nil {
			return specErrorf("join", *spec.Join, "only valid when format is ndjson")
		}
	}

	if err := validateParse(spec.Parse, format); err != nil {
		return err
	}
	for i, cond := range spec.Select {
		if err := validateLineCondition("select", i, cond); err != nil {
			return err
		}
	}
	for i, cond := range spec.Success {
		if err := validateLineCondition("success", i, cond); err != nil {
			return err
		}
	}
	return nil
}

// ValidateReviewOutput rejects source=stderr.
func ValidateReviewOutput(spec OutputSpec) error {
	if err := ValidateOutput(spec); err != nil {
		return err
	}
	if spec.Source == SourceStderr {
		return specErrorf("source", spec.Source, "not allowed for review")
	}
	return nil
}

// ValidateTerminalOutput rejects source=file.
func ValidateTerminalOutput(spec OutputSpec) error {
	if err := ValidateOutput(spec); err != nil {
		return err
	}
	if spec.Source == SourceFile {
		return specErrorf("source", spec.Source, "not allowed for terminal")
	}
	return nil
}

func (s OutputSpec) effectiveFormat() string {
	if s.Format == "" {
		return FormatJSON
	}
	return s.Format
}

func (s OutputSpec) effectiveJoin() string {
	if s.Join == nil {
		return "\n"
	}
	return *s.Join
}

func validateParse(parse, format string) error {
	switch {
	case parse == ParseRaw:
		if format == FormatNDJSON {
			return specErrorf("parse", parse, "raw is not valid with format ndjson")
		}
		return nil
	case strings.HasPrefix(parse, parsePrefixJSONField):
		path := strings.TrimPrefix(parse, parsePrefixJSONField)
		if err := validateDottedPath(path); err != nil {
			return specErrorf("parse", parse, "%s", err.Error())
		}
		return nil
	case strings.HasPrefix(parse, parsePrefixRegex):
		pattern := strings.TrimPrefix(parse, parsePrefixRegex)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return specErrorf("parse", parse, "regex does not compile: %s", err.Error())
		}
		if re.NumSubexp() != 1 {
			return specErrorf("parse", parse, "regex must have exactly one capturing group")
		}
		return nil
	default:
		return specErrorf("parse", parse, "must be raw, json_field:<dotted.path>, or regex:<pattern>")
	}
}

func validateDottedPath(path string) error {
	if path == "" {
		return errors.New("dotted path is empty")
	}
	if strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return errors.New("dotted path contains a control character")
	}
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return fmt.Errorf("dotted path %q has an empty segment", path)
		}
	}
	return nil
}

func validateLineCondition(field string, index int, cond LineCondition) error {
	label := fmt.Sprintf("%s[%d].json_field", field, index)
	if err := validateDottedPath(cond.JSONField); err != nil {
		return specErrorf(label, cond.JSONField, "%s", err.Error())
	}
	hasEquals := len(cond.Equals) > 0
	hasAbsent := cond.Absent != nil
	switch {
	case hasEquals && hasAbsent:
		return specErrorf(label, cond.JSONField, "equals and absent cannot both be set")
	case !hasEquals && !hasAbsent:
		return specErrorf(label, cond.JSONField, "must set equals or absent")
	case hasAbsent && !*cond.Absent:
		return specErrorf(label, "false", "absent must be true")
	}
	if hasEquals && !json.Valid(cond.Equals) {
		return specErrorf(fmt.Sprintf("%s[%d].equals", field, index), string(cond.Equals), "must be a valid JSON value")
	}
	return nil
}

// ParseOutput validates the full source set and evaluates data.
// The caller already obtained stdout, stderr, or the result file and passes that text.
func ParseOutput(spec OutputSpec, data string) (string, error) {
	if err := ValidateOutput(spec); err != nil {
		return "", err
	}
	return evaluateOutput(spec, data)
}

// ParseReviewOutput is the review-subset entry: source stderr is rejected.
func ParseReviewOutput(spec OutputSpec, data string) (string, error) {
	if err := ValidateReviewOutput(spec); err != nil {
		return "", err
	}
	return evaluateOutput(spec, data)
}

// ParseTerminalOutput is the terminal-subset entry: source file is rejected.
func ParseTerminalOutput(spec OutputSpec, data string) (string, error) {
	if err := ValidateTerminalOutput(spec); err != nil {
		return "", err
	}
	return evaluateOutput(spec, data)
}

func evaluateOutput(spec OutputSpec, data string) (string, error) {
	if spec.effectiveFormat() == FormatNDJSON {
		return evaluateNDJSON(spec, data)
	}
	return evaluateJSON(spec, data)
}

func evaluateJSON(spec OutputSpec, data string) (string, error) {
	var doc any
	err := json.Unmarshal([]byte(data), &doc)
	if len(spec.Success) > 0 {
		if err != nil {
			return "", fmt.Errorf("%w: document is not JSON", ErrSuccess)
		}
		if !matchAll(doc, spec.Success) {
			return "", ErrSuccess
		}
	}
	return extractText(spec.Parse, data, doc, err)
}

type ndjsonRow struct {
	raw string
	doc any
}

func evaluateNDJSON(spec OutputSpec, data string) (string, error) {
	var rows []ndjsonRow
	for _, line := range splitOutputLines(data) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var doc any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			continue
		}
		rows = append(rows, ndjsonRow{raw: line, doc: doc})
	}
	if len(spec.Success) > 0 {
		ok := false
		for _, row := range rows {
			if matchAll(row.doc, spec.Success) {
				ok = true
				break
			}
		}
		if !ok {
			return "", ErrSuccess
		}
	}
	var parts []string
	for _, row := range rows {
		if len(spec.Select) > 0 && !matchAll(row.doc, spec.Select) {
			continue
		}
		text, err := extractText(spec.Parse, row.raw, row.doc, nil)
		if err != nil {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, spec.effectiveJoin()), nil
}

func splitOutputLines(data string) []string {
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
}

func matchAll(doc any, conds []LineCondition) bool {
	for _, cond := range conds {
		if !matchCondition(doc, cond) {
			return false
		}
	}
	return true
}

func matchCondition(doc any, cond LineCondition) bool {
	value, ok := walkPath(doc, cond.JSONField)
	if cond.Absent != nil && *cond.Absent {
		return !ok
	}
	if !ok || len(cond.Equals) == 0 {
		return false
	}
	var want any
	if err := json.Unmarshal(cond.Equals, &want); err != nil {
		return false
	}
	return jsonValuesEqual(value, want)
}

func walkPath(doc any, path string) (any, bool) {
	cur := doc
	for _, key := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[key]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func jsonValuesEqual(a, b any) bool {
	return jsonEqual(normalizeJSON(a), normalizeJSON(b))
}

func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for key, value := range av {
			other, ok := bv[key]
			if !ok || !jsonEqual(value, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		ab, err1 := json.Marshal(a)
		bb, err2 := json.Marshal(b)
		if err1 != nil || err2 != nil {
			return false
		}
		return bytes.Equal(ab, bb)
	}
}

func normalizeJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

func extractText(parse, raw string, doc any, decodeErr error) (string, error) {
	switch {
	case parse == ParseRaw:
		return raw, nil
	case strings.HasPrefix(parse, parsePrefixJSONField):
		if decodeErr != nil {
			return "", decodeErr
		}
		path := strings.TrimPrefix(parse, parsePrefixJSONField)
		value, ok := walkPath(doc, path)
		if !ok {
			return "", specErrorf("parse", parse, "json field is absent")
		}
		text, ok := value.(string)
		if !ok {
			return "", specErrorf("parse", parse, "json field is not a string")
		}
		return text, nil
	case strings.HasPrefix(parse, parsePrefixRegex):
		re := regexp.MustCompile(strings.TrimPrefix(parse, parsePrefixRegex))
		match := re.FindStringSubmatch(raw)
		if len(match) != 2 {
			return "", specErrorf("parse", parse, "regex did not match")
		}
		return match[1], nil
	default:
		return "", specErrorf("parse", parse, "unsupported parse")
	}
}
