package process

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustSpec(t *testing.T, raw string) OutputSpec {
	t.Helper()
	spec, err := DecodeOutputSpec([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeOutputSpec(%s): %v", raw, err)
	}
	return spec
}

func joinPtr(s string) *string { return &s }

func TestValidateOutputRejectsIllegalCombinations(t *testing.T) {
	tests := []struct {
		name  string
		spec  OutputSpec
		field string
		value string
	}{
		{"source", OutputSpec{Source: "pipe", Parse: ParseRaw}, "source", "pipe"},
		{"format", OutputSpec{Source: SourceStdout, Format: "yaml", Parse: ParseRaw}, "format", "yaml"},
		{"select-on-json", OutputSpec{Source: SourceStdout, Parse: "json_field:a", Select: []LineCondition{{JSONField: "a", Equals: json.RawMessage(`"x"`)}}}, "select", "present"},
		{"empty-select-on-json", OutputSpec{Source: SourceStdout, Parse: ParseRaw, Select: []LineCondition{}}, "select", "present"},
		{"join-on-json", OutputSpec{Source: SourceStdout, Parse: ParseRaw, Join: joinPtr("|")}, "join", "|"},
		{"ndjson-raw", OutputSpec{Source: SourceStdout, Format: FormatNDJSON, Parse: ParseRaw}, "parse", "raw"},
		{"bad-regex", OutputSpec{Source: SourceStdout, Parse: "regex:("}, "parse", "regex:("},
		{"regex-no-group", OutputSpec{Source: SourceStdout, Parse: "regex:abc"}, "parse", "regex:abc"},
		{"empty-path", OutputSpec{Source: SourceStdout, Parse: "json_field:"}, "parse", "json_field:"},
		{"empty-segment", OutputSpec{Source: SourceStdout, Parse: "json_field:a..b"}, "parse", "json_field:a..b"},
		{"unknown-parse", OutputSpec{Source: SourceStdout, Parse: "jq:.a"}, "parse", "jq:.a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutput(tt.spec)
			var specErr *SpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("err = %v", err)
			}
			if specErr.Field != tt.field {
				t.Fatalf("field = %q, want %q (%v)", specErr.Field, tt.field, err)
			}
			if !strings.Contains(err.Error(), tt.field) || !strings.Contains(err.Error(), tt.value) {
				t.Fatalf("error %q must contain field %q and value %q", err, tt.field, tt.value)
			}
		})
	}
}

func TestValidateLineConditionStructure(t *testing.T) {
	trueAbsent := true
	falseAbsent := false
	equals := json.RawMessage(`"ok"`)
	tests := []struct {
		name string
		cond LineCondition
	}{
		{"neither", LineCondition{JSONField: "role"}},
		{"both", LineCondition{JSONField: "role", Equals: equals, Absent: &trueAbsent}},
		{"absent-false", LineCondition{JSONField: "role", Absent: &falseAbsent}},
		{"empty-field", LineCondition{JSONField: "", Equals: equals}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutput(OutputSpec{
				Source:  SourceStdout,
				Format:  FormatNDJSON,
				Parse:   "json_field:content",
				Success: []LineCondition{tt.cond},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "success") && !strings.Contains(err.Error(), "json_field") {
				t.Fatalf("error %q must name the field", err)
			}
		})
	}
	if err := ValidateOutput(OutputSpec{
		Source: SourceStdout,
		Parse:  "json_field:is_error",
		Success: []LineCondition{
			{JSONField: "is_error", Equals: json.RawMessage(`false`)},
			{JSONField: "type", Equals: json.RawMessage(`null`)},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConditionEqualsJSONValues(t *testing.T) {
	cases := []struct {
		raw   string
		valid bool
	}{
		{`null`, true}, {`true`, true}, {`1.5`, true}, {`"ok"`, true},
		{`[1,null]`, true}, {`{"ok":true}`, true},
		{`not-json`, false}, {`true false`, false}, {`{"ok":`, false},
	}
	for _, field := range []string{"select", "success"} {
		for _, tt := range cases {
			t.Run(field+"/"+tt.raw, func(t *testing.T) {
				spec := OutputSpec{Source: SourceStdout, Format: FormatNDJSON, Parse: "json_field:content"}
				conditions := []LineCondition{{JSONField: "value", Equals: json.RawMessage(tt.raw)}}
				if field == "select" {
					spec.Select = conditions
				} else {
					spec.Success = conditions
				}
				err := ValidateOutput(spec)
				if tt.valid {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				var specErr *SpecError
				if !errors.As(err, &specErr) || specErr.Field != field+"[0].equals" || specErr.Value != tt.raw {
					t.Fatalf("invalid equals must identify its field and value: %v", err)
				}
			})
		}
	}
}

func TestDecodeOutputSpecRejectsEmptySelectOnJSON(t *testing.T) {
	_, err := DecodeOutputSpec([]byte(`{"source":"stdout","parse":"raw","select":[]}`))
	if err == nil || !strings.Contains(err.Error(), "select") {
		t.Fatalf("explicit empty select must be rejected on json: %v", err)
	}
	if _, err := DecodeOutputSpec([]byte(`{"source":"stdout","format":"ndjson","parse":"json_field:content","select":[]}`)); err != nil {
		t.Fatalf("empty select remains valid on ndjson: %v", err)
	}
}

func TestReviewAndTerminalSourceSubsets(t *testing.T) {
	reviewStderr := OutputSpec{Source: SourceStderr, Parse: ParseRaw}
	if err := ValidateReviewOutput(reviewStderr); err == nil || !strings.Contains(err.Error(), "source") || !strings.Contains(err.Error(), "stderr") {
		t.Fatalf("review must reject stderr: %v", err)
	}
	if _, err := ParseReviewOutput(reviewStderr, "x"); err == nil || !strings.Contains(err.Error(), "stderr") {
		t.Fatalf("ParseReviewOutput must reject stderr: %v", err)
	}
	if err := ValidateReviewOutput(OutputSpec{Source: SourceFile, Parse: ParseRaw}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReviewOutput(OutputSpec{Source: SourceStdout, Parse: ParseRaw}); err != nil {
		t.Fatal(err)
	}

	terminalFile := OutputSpec{Source: SourceFile, Parse: ParseRaw}
	if err := ValidateTerminalOutput(terminalFile); err == nil || !strings.Contains(err.Error(), "source") || !strings.Contains(err.Error(), "file") {
		t.Fatalf("terminal must reject file: %v", err)
	}
	if _, err := ParseTerminalOutput(terminalFile, "x"); err == nil || !strings.Contains(err.Error(), "file") {
		t.Fatalf("ParseTerminalOutput must reject file: %v", err)
	}
	if err := ValidateTerminalOutput(OutputSpec{Source: SourceStderr, Parse: ParseRaw}); err != nil {
		t.Fatal(err)
	}
}

func TestNDJSONFourStepEvaluation(t *testing.T) {
	spec := mustSpec(t, `{
		"source":"stdout",
		"format":"ndjson",
		"select":[{"json_field":"role","equals":"assistant"}],
		"parse":"json_field:content"
	}`)
	input := strings.Join([]string{
		"",
		"not-json",
		`{"role":"assistant","content":"one"}`,
		`{"role":"tool","content":"SKIP"}`,
		`{"role":"assistant","content":"two"`,
		`{"role":"assistant","content":"three"}`,
	}, "\n")
	got, err := ParseOutput(spec, input)
	if err != nil {
		t.Fatal(err)
	}
	if got != "one\nthree" {
		t.Fatalf("got %q", got)
	}

	joined := spec
	comma := ","
	joined.Join = &comma
	got, err = ParseOutput(joined, input)
	if err != nil {
		t.Fatal(err)
	}
	if got != "one,three" {
		t.Fatalf("override join: %q", got)
	}
}

func TestNDJSONSelectDropsSameFieldPollution(t *testing.T) {
	spec := mustSpec(t, `{
		"source":"stdout",
		"format":"ndjson",
		"select":[{"json_field":"role","equals":"assistant"}],
		"parse":"json_field:content"
	}`)
	input := strings.Join([]string{
		`{"role":"assistant","content":"# Title"}`,
		`{"role":"tool","content":"TOOL_BODY"}`,
		`{"role":"assistant","content":"## Section"}`,
		`{"role":"meta","type":"session.resume_hint","content":"To resume this session: abc"}`,
	}, "\n")
	got, err := ParseOutput(spec, input)
	if err != nil {
		t.Fatal(err)
	}
	if got != "# Title\n## Section" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "TOOL_BODY") || strings.Contains(got, "To resume this session") {
		t.Fatalf("pollution leaked: %q", got)
	}
}

func TestNDJSONSuccessIsExistentialConjunction(t *testing.T) {
	spec := mustSpec(t, `{
		"source":"stdout",
		"format":"ndjson",
		"parse":"json_field:content",
		"success":[
			{"json_field":"is_error","equals":false},
			{"json_field":"type","equals":"result"}
		]
	}`)
	split := strings.Join([]string{
		`{"is_error":true,"type":"result","content":"bad"}`,
		`{"is_error":false,"type":"progress","content":"ok"}`,
	}, "\n")
	if _, err := ParseOutput(spec, split); !errors.Is(err, ErrSuccess) {
		t.Fatalf("split conditions must fail: %v", err)
	}

	same := `{"is_error":false,"type":"result","content":"report"}`
	got, err := ParseOutput(spec, same)
	if err != nil || got != "report" {
		t.Fatalf("same-line success: %q %v", got, err)
	}
}

func TestNDJSONAbsentIsPerLine(t *testing.T) {
	spec := mustSpec(t, `{
		"source":"stdout",
		"format":"ndjson",
		"parse":"json_field:content",
		"success":[{"json_field":"is_error","absent":true}]
	}`)
	input := strings.Join([]string{
		`{"is_error":false,"content":"has-field"}`,
		`{"content":"no-error-field"}`,
	}, "\n")
	got, err := ParseOutput(spec, input)
	if err != nil || got != "has-field\nno-error-field" {
		t.Fatalf("got %q %v", got, err)
	}

	onlyPresent := `{"is_error":true,"content":"x"}` + "\n" + `{"is_error":false,"content":"y"}`
	if _, err := ParseOutput(spec, onlyPresent); !errors.Is(err, ErrSuccess) {
		t.Fatalf("absent must not be vacuously true: %v", err)
	}

	spec.Success = append(spec.Success, LineCondition{JSONField: "type", Equals: json.RawMessage(`"result"`)})
	split := `{"type":"result","is_error":true,"content":"failed"}` + "\n" + `{"type":"progress","content":"not a result"}`
	if _, err := ParseOutput(spec, split); !errors.Is(err, ErrSuccess) {
		t.Fatalf("absent and equals on different rows must fail: %v", err)
	}
	got, err = ParseOutput(spec, `{"type":"result","content":"complete"}`)
	if err != nil || got != "complete" {
		t.Fatalf("same-row absent and equals must succeed: %q %v", got, err)
	}
}

func TestNDJSONSuccessIgnoresSelect(t *testing.T) {
	spec := mustSpec(t, `{
		"source":"stdout",
		"format":"ndjson",
		"select":[{"json_field":"role","equals":"assistant"}],
		"parse":"json_field:content",
		"success":[
			{"json_field":"type","equals":"result"},
			{"json_field":"is_error","equals":false}
		]
	}`)
	input := strings.Join([]string{
		`{"role":"assistant","content":"body"}`,
		`{"role":"result","type":"result","is_error":false,"content":"IGNORED"}`,
	}, "\n")
	got, err := ParseOutput(spec, input)
	if err != nil || got != "body" {
		t.Fatalf("got %q %v", got, err)
	}
	if strings.Contains(got, "IGNORED") {
		t.Fatalf("result row leaked through select: %q", got)
	}
}

func TestJSONSuccessFailsWhenDocumentIsNotJSON(t *testing.T) {
	spec := mustSpec(t, `{
		"source":"file",
		"parse":"raw",
		"success":[{"json_field":"ok","equals":true}]
	}`)
	if _, err := ParseOutput(spec, "not-json"); !errors.Is(err, ErrSuccess) {
		t.Fatalf("err = %v", err)
	}
	got, err := ParseOutput(OutputSpec{Source: SourceFile, Parse: ParseRaw}, "not-json")
	if err != nil || got != "not-json" {
		t.Fatalf("raw without success: %q %v", got, err)
	}
}

func TestPaperReviewerExpressions(t *testing.T) {
	tests := []struct {
		name string
		spec string
		in   string
		want string
		fail bool
	}{
		{
			name: "codex-raw-file",
			spec: `{"source":"file","parse":"raw"}`,
			in:   "# Review\nPASS",
			want: "# Review\nPASS",
		},
		{
			name: "claude-cursor-success",
			spec: `{"source":"file","parse":"json_field:result","success":[{"json_field":"type","equals":"result"},{"json_field":"subtype","equals":"success"},{"json_field":"is_error","equals":false}]}`,
			in:   `{"type":"result","subtype":"success","is_error":false,"result":"ok"}`,
			want: "ok",
		},
		{
			name: "claude-cursor-is-error",
			spec: `{"source":"file","parse":"json_field:result","success":[{"json_field":"type","equals":"result"},{"json_field":"subtype","equals":"success"},{"json_field":"is_error","equals":false}]}`,
			in:   `{"type":"result","subtype":"success","is_error":true,"result":"looks-like-report"}`,
			fail: true,
		},
		{
			name: "grok-stopReason",
			spec: `{"source":"file","parse":"json_field:text","success":[{"json_field":"stopReason","equals":"end_turn"}]}`,
			in:   `{"stopReason":"end_turn","text":"ok"}`,
			want: "ok",
		},
		{
			name: "grok-stopped-early",
			spec: `{"source":"file","parse":"json_field:text","success":[{"json_field":"stopReason","equals":"end_turn"}]}`,
			in:   `{"stopReason":"max_tokens","text":"looks-like-report"}`,
			fail: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := mustSpec(t, tt.spec)
			if err := ValidateReviewOutput(spec); err != nil {
				t.Fatal(err)
			}
			got, err := ParseReviewOutput(spec, tt.in)
			if tt.fail {
				if !errors.Is(err, ErrSuccess) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
}

func TestJSONFieldAndRegexPrimitives(t *testing.T) {
	doc := `{"type":"result","subtype":"success","is_error":false,"result":"TEXT"}`
	spec := mustSpec(t, `{
		"source":"file",
		"parse":"json_field:result",
		"success":[
			{"json_field":"type","equals":"result"},
			{"json_field":"subtype","equals":"success"},
			{"json_field":"is_error","equals":false}
		]
	}`)
	got, err := ParseOutput(spec, doc)
	if err != nil || got != "TEXT" {
		t.Fatalf("claude-shaped: %q %v", got, err)
	}

	re := mustSpec(t, `{"source":"stdout","parse":"regex:id=([A-Za-z0-9-]+)"}`)
	got, err = ParseOutput(re, "prefix id=abc-1 suffix")
	if err != nil || got != "abc-1" {
		t.Fatalf("regex: %q %v", got, err)
	}
}

func TestDecodeOutputSpecRejectsUnknownFields(t *testing.T) {
	_, err := DecodeOutputSpec([]byte(`{"source":"stdout","parse":"raw","extra":true}`))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}
