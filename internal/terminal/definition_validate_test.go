package terminal

import (
	"context"
	"strings"
	"testing"
)

func TestFixtureDefinitionIsValid(t *testing.T) {
	resetLanguage(t)
	if _, err := DecodeDefinition("/share/terminals/faketerm.json", []byte(fixtureDefinition)); err != nil {
		t.Fatal(err)
	}
}

// Each case changes one thing of the valid fixture; the error must name the
// file and locate the problem by operation, step and field.
func TestDefinitionValidationRejects(t *testing.T) {
	resetLanguage(t)
	RegisterHook("validate-test-hook", func(context.Context, HookCall) HookResult { return HookResult{Status: HookOK} })
	cases := []struct {
		name   string
		change func(root map[string]any)
		want   []string
	}{
		{"unknown schema version", func(r map[string]any) { r["schema_version"] = 2 }, []string{"field schema_version", "unknown schema version 2"}},
		{"unknown top-level field", func(r map[string]any) { r["scripts"] = []any{} }, []string{`unknown field "scripts"`}},
		{"name differs from file", func(r map[string]any) { r["name"] = "other" }, []string{"field name", "does not match the file name"}},
		{"required operation missing", func(r map[string]any) { delete(object(r, "ops"), "reverse_lookup") }, []string{"required operation reverse_lookup is missing"}},
		{"capability operation missing", func(r map[string]any) { delete(object(r, "ops"), "focus") }, []string{"required operation focus is missing"}},
		{"operation without capability", func(r map[string]any) { object(r, "capabilities")["wait_output"] = false }, []string{"operation wait_output is declared but its capability is false"}},
		{"unknown operation", func(r map[string]any) { object(r, "ops")["attach"] = map[string]any{} }, []string{"field ops.attach", "unknown operation"}},
		{"empty steps", func(r map[string]any) { object(r, "ops", "run_command")["steps"] = []any{} }, []string{"op run_command", "field steps", "must not be empty"}},
		{"output source file", func(r map[string]any) {
			firstStep(r, "read_output")["output"] = map[string]any{"source": "file", "parse": "raw"}
		}, []string{"op read_output", "step steps[0]", "field output", "not allowed for terminal"}},
		{"output ndjson", func(r map[string]any) {
			firstStep(r, "read_output")["output"] = map[string]any{"source": "stdout", "format": "ndjson", "parse": "json_field:text"}
		}, []string{"field output.format", "whole-document form only"}},
		{"field primitive", func(r map[string]any) { firstStep(r, "topology")["fields"] = map[string]any{"count": "xpath:/a"} }, []string{"field fields.count", "must be raw"}},
		{"field regex groups", func(r map[string]any) { firstStep(r, "topology")["fields"] = map[string]any{"count": "regex:(a)(b)"} }, []string{"exactly one capturing group"}},
		{"reserved field name", func(r map[string]any) { firstStep(r, "topology")["fields"] = map[string]any{"ok": "raw"} }, []string{"field fields.ok", "reserved"}},
		{"fields without store", func(r map[string]any) { delete(firstStep(r, "topology"), "store") }, []string{"field store", "need a store name"}},
		{"bad poll until", func(r map[string]any) { object(firstStep(r, "wait_ready"), "poll")["until"] = "forever" }, []string{"op wait_ready", "field poll.until"}},
		{"matched outside wait_output", func(r map[string]any) { object(firstStep(r, "wait_ready"), "poll")["until"] = "matched" }, []string{"field poll.until", "only valid there"}},
		{"bad poll interval", func(r map[string]any) { object(firstStep(r, "wait_ready"), "poll")["interval"] = "soon" }, []string{"field poll.interval", "invalid duration"}},
		{"timeout_ms outside wait_output", func(r map[string]any) { object(firstStep(r, "wait_ready"), "poll")["timeout"] = "{timeout_ms}" }, []string{"field poll.timeout"}},
		{"bad on_error", func(r map[string]any) { firstStep(r, "run_command")["on_error"] = "ignore" }, []string{"field on_error", "must be fail, continue or meta_missing"}},
		{"bad step timeout", func(r map[string]any) { firstStep(r, "run_command")["timeout"] = "-1s" }, []string{"field timeout", "must be positive"}},
		{"stop_on_success type", func(r map[string]any) { firstStep(r, "run_command")["stop_on_success"] = "yes" }, []string{"stop_on_success"}},
		{"when references later store", func(r map[string]any) {
			step := firstStep(r, "pane_facts")
			step["when"] = "field:step.marker.text=x"
		}, []string{"op pane_facts", "step steps[0]", "field when", `unknown name "step.marker.text"`}},
		{"when unknown form", func(r map[string]any) { firstStep(r, "run_command")["when"] = "always" }, []string{"field when", "unknown condition"}},
		{"store name", func(r map[string]any) { firstStep(r, "read_output")["store"] = "Out" }, []string{"field store", "must match"}},
		{"argv and fail", func(r map[string]any) { firstStep(r, "run_command")["fail"] = "nope" }, []string{"exactly one of argv and fail"}},
		{"unknown placeholder", func(r map[string]any) { firstStep(r, "run_command")["argv"] = []any{"run", "{window}"} }, []string{"field argv[1]", "unknown placeholder {window}"}},
		{"unescaped brace", func(r map[string]any) { firstStep(r, "run_command")["argv"] = []any{"run", "#{session_id}"} }, []string{"field argv[1]", "unknown placeholder {session_id}"}},
		{"newline in template", func(r map[string]any) { firstStep(r, "run_command")["argv"] = []any{"run", "a\nb"} }, []string{"field argv[1]", "control character U+000A"}},
		{"empty argv element", func(r map[string]any) { firstStep(r, "run_command")["argv"] = []any{"run", ""} }, []string{"field argv[1]", "empty argv element"}},
		{"unknown message id", func(r map[string]any) {
			firstStep(r, "run_command")["messages"] = map[string]any{"exit": map[string]any{"id": "launch.no_such_message"}}
		}, []string{"field messages.exit", "unknown message id"}},
		{"candidates without values", func(r map[string]any) {
			object(r, "ops")["prepare"] = map[string]any{"candidates": map[string]any{"values": []any{}, "steps": []any{}, "select": []any{}}}
		}, []string{"op prepare", "field candidates.values"}},
		{"candidate select result field", func(r map[string]any) {
			object(r, "ops")["prepare"] = map[string]any{"candidates": map[string]any{
				"values": []any{"a"}, "steps": []any{map[string]any{"argv": []any{"probe", "{candidate}"}}},
				"select": []any{map[string]any{"when": "prev_ok", "result": map[string]any{"window": "{candidate}"}}},
			}}
		}, []string{"field candidates.select[0].result.window", "is not a result field"}},
		{"fallback sees candidate stores", func(r map[string]any) {
			object(r, "ops")["prepare"] = map[string]any{"candidates": map[string]any{
				"values": []any{"a"}, "steps": []any{map[string]any{"store": "probe", "argv": []any{"probe", "{candidate}"}}},
				"select":   []any{map[string]any{"when": "prev_ok", "result": map[string]any{"session": "{candidate}"}}},
				"fallback": []any{map[string]any{"fail": "{step.probe.detail}"}},
			}}
		}, []string{"step candidates.fallback[0]", "unknown placeholder {step.probe.detail}"}},
		{"requires platform", func(r map[string]any) { object(r, "launchers", "faketerm", "requires")["platform"] = "linux" }, []string{"field launchers.faketerm.requires.platform"}},
		{"requires env name", func(r map[string]any) {
			object(r, "launchers", "faketerm", "requires")["env"] = []any{map[string]any{"name": "BAD-NAME"}}
		}, []string{"field launchers.faketerm.requires.env[0].name"}},
		{"auto outside a session", func(r map[string]any) { object(r, "launchers", "faketerm", "requires")["inside_session"] = false }, []string{"field launchers.faketerm.auto_priority"}},
		{"launcher name", func(r map[string]any) {
			launchers := object(r, "launchers")
			launchers["Fake:Term"] = launchers["faketerm"]
			delete(launchers, "faketerm")
		}, []string{"field launchers.Fake:Term", "launcher name must match"}},
		{"unregistered hook", func(r map[string]any) { r["hooks"] = map[string]any{"focus_pane": "no-such-hook"} }, []string{"field hooks.focus_pane", `hook "no-such-hook" is not registered`}},
		{"unknown hook point", func(r map[string]any) { r["hooks"] = map[string]any{"attach": "validate-test-hook"} }, []string{"field hooks.attach", "unknown mount point"}},
		{"focus operation is not a mount point", func(r map[string]any) { r["hooks"] = map[string]any{"focus": "validate-test-hook"} }, []string{"field hooks.focus", "unknown mount point (report_session, focus_pane)"}},
		{"rows split", func(r map[string]any) { object(r, "ops", "reverse_lookup", "rows")["split"] = "json" }, []string{"field rows.split", "json_array:<dotted.path>"}},
		{"topology row result field", func(r map[string]any) {
			object(r, "ops", "topology")["rows"] = map[string]any{"from": "topo", "fields": map[string]any{"pane": "raw"}, "match": "prev_ok", "result": map[string]any{"session": "{row.pane}", "pane": "{row.pane}"}}
		}, []string{"op topology", "field rows.result.session", "is not a row result field of topology"}},
		{"session report without hook", func(r map[string]any) { object(r, "capabilities")["session_report"] = true }, []string{"field hooks.report_session"}},
		{"error rule kind", func(r map[string]any) { object(r, "errors")["gone"] = []any{"signal:9"} }, []string{"field errors.gone[0]", "unknown kind"}},
		{"error rule regex", func(r map[string]any) { object(r, "errors")["meta_missing"] = []any{"stderr:(unclosed"} }, []string{"field errors.meta_missing[0]", "does not compile"}},
		{"error rule json", func(r map[string]any) { object(r, "errors")["gone"] = []any{"stdout_json:error.code"} }, []string{"field errors.gone[0]"}},
		{"address field name", func(r map[string]any) {
			r["address"] = []any{map[string]any{"name": "tab"}, map[string]any{"name": "pane"}}
		}, []string{"field address[0].name"}},
		{"address without container", func(r map[string]any) { r["address"] = []any{map[string]any{"name": "pane"}} }, []string{"field address", "must include container and pane"}},
		{"address pattern group", func(r map[string]any) {
			r["address"] = []any{map[string]any{"name": "container", "pattern": "(w\\d+)"}, map[string]any{"name": "pane"}}
		}, []string{"field address[0].pattern", "capturing groups"}},
		{"container capability", func(r map[string]any) { object(r, "capabilities")["container"] = false }, []string{"field capabilities.container"}},
		{"gone_when outside facts", func(r map[string]any) {
			step := firstStep(r, "read_output")
			step["gone_when"] = "field_missing:step.out.text"
		}, []string{"op read_output", "field gone_when", "only pane_facts and container_exists"}},
		{"gone message without gone_when", func(r map[string]any) {
			firstStep(r, "read_output")["messages"] = map[string]any{"gone": "closed"}
		}, []string{"field messages.gone", "only used with gone_when"}},
		{"version args placeholder", func(r map[string]any) { r["version_args"] = []any{"{env.HOME}"} }, []string{"field version_args[0]", "take no placeholders"}},
		{"requires env message", func(r map[string]any) {
			object(r, "launchers", "faketerm", "requires")["env"] = []any{map[string]any{"name": "FAKETERM", "message": "{window}"}}
		}, []string{"field launchers.faketerm.requires.env[0].message", "unknown placeholder {window}"}},
		{"row check without condition", func(r map[string]any) {
			object(r, "ops", "reverse_lookup", "rows")["checks"] = []any{map[string]any{"message": "bad"}}
		}, []string{"field rows.checks[0].when", "must not be empty"}},
		{"binary relative path", func(r map[string]any) { r["binary"] = "bin/faketerm" }, []string{"field binary"}},
		{"rows outside reverse lookup", func(r map[string]any) {
			object(r, "ops", "read_output")["rows"] = object(r, "ops", "reverse_lookup")["rows"]
		}, []string{"op read_output", "field rows"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeDefinition("/share/terminals/faketerm.json", mutateFixture(t, tc.change))
			if err == nil {
				t.Fatal("definition accepted")
			}
			for _, want := range append([]string{"/share/terminals/faketerm.json"}, tc.want...) {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestDefinitionTemplateRules(t *testing.T) {
	resetLanguage(t)
	accepted := mutateFixture(t, func(r map[string]any) {
		firstStep(r, "read_output")["argv"] = []any{"display", "-F", "#{{session_id}}\t#{{pane_id}}", "{{literal}}", "{pane}"}
	})
	def, err := DecodeDefinition("faketerm.json", accepted)
	if err != nil {
		t.Fatalf("escaped braces and TAB rejected: %v", err)
	}
	backend, err := NewDeclarativeBackend(def, "faketerm", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	conn := recordingConn(&got)
	if _, err := backend.ReadOutput(context.Background(), conn, "%1 {not a placeholder}"); err != nil {
		t.Fatal(err)
	}
	want := []string{"display", "-F", "#{session_id}\t#{pane_id}", "{literal}", "%1 {not a placeholder}"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("argv=%q want %q", got, want)
	}
	for _, value := range []string{"line\nbreak", "carriage\rreturn", "nul\x00byte"} {
		err := backend.DeliverText(context.Background(), conn, "%1", value)
		if err == nil || err.Error() != "the value of placeholder text contains a line break or NUL" {
			t.Fatalf("runtime value %q: err=%v", value, err)
		}
	}
	if err := backend.DeliverText(context.Background(), conn, "%1", "tab\tand {braces}"); err != nil {
		t.Fatalf("runtime TAB or braces rejected: %v", err)
	}
}
