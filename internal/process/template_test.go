package process

import (
	"strings"
	"testing"
)

func TestExpandTemplateEscapesLiteralBraces(t *testing.T) {
	names := []string{"inspection", "prompt"}
	template := "---\nallow: {{read}}\n---\n# Guide\nUse `{{path}}` as `{{path}}`.\n{inspection}\n\n```json\n{{\"role\":\"assistant\"}}\n```\n{prompt}\n"
	values := map[string]string{
		"inspection": "no writes",
		"prompt":     "review the diff",
	}
	got, err := ExpandTemplate(template, values, names)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nallow: {read}\n---\n# Guide\nUse `{path}` as `{path}`.\nno writes\n\n```json\n{\"role\":\"assistant\"}\n```\nreview the diff\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if err := ValidateTemplate(template, names, TemplateText); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownPlaceholderNamesThePlaceholder(t *testing.T) {
	_, err := ExpandTemplate("hello {world}", nil, []string{"model"})
	if err == nil || !strings.Contains(err.Error(), "placeholder") || !strings.Contains(err.Error(), "world") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateTemplate("hello {world}", []string{"model"}, TemplateArgv); err == nil || !strings.Contains(err.Error(), "world") {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateArgvRejectsEmptyAndControl(t *testing.T) {
	names := []string{"model"}
	if err := ValidateArgv([]string{"--model", ""}, names, TemplateArgv); err == nil || !strings.Contains(err.Error(), "argv") {
		t.Fatalf("empty: %v", err)
	}
	if err := ValidateArgv([]string{"--model", "{model}"}, names, TemplateArgv); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplate("bad\x00arg", names, TemplateArgv); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("nul: %v", err)
	}
	if err := ValidateTemplate("a\tb", names, TemplateArgv); err == nil {
		t.Fatal("argv must reject TAB")
	}
	if err := ValidateTemplate("a\tb", names, TemplateArgvTab); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplate("line\n{model}", names, TemplateText); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplate("line\n{model}", names, TemplateArgv); err == nil {
		t.Fatal("argv must reject newline")
	}
}

func TestExpandTemplateIsNotRecursive(t *testing.T) {
	got, err := ExpandTemplate("{model}", map[string]string{"model": "{effort}"}, []string{"model", "effort"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "{effort}" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandArgvOmitEmptyDropsFlagAndKeepsEscapes(t *testing.T) {
	names := []string{"model", "effort"}
	got, err := ExpandArgvOmitEmpty([]string{"--model", "{model}", "--config", `effort="{effort}"`, "--keep", "{{model}}"}, map[string]string{"model": "", "effort": "high"}, names)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--config", `effort="high"`, "--keep", "{model}"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestExpandTemplateMissingValueIsEmpty(t *testing.T) {
	got, err := ExpandTemplate("x{model}y", nil, []string{"model"})
	if err != nil || got != "xy" {
		t.Fatalf("got %q %v", got, err)
	}
}
