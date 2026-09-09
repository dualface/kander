package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewTemplateValidation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	validOutput := map[string]any{"source": "file", "parse": "raw"}
	validReview := map[string]any{
		"cwd": "runtime", "output_name": "out.txt", "output": validOutput, "stdin": "instruction",
	}
	base, _ := json.Marshal(DefaultConfig())
	for _, test := range []struct {
		name string
		def  map[string]any
		bad  bool
		want string
	}{
		{"pair-args-only", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--x"}}, "session": map[string]any{"mode": "none"}}, true, "together"},
		{"pair-review-only", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}}, "session": map[string]any{"mode": "none"}, "review": validReview}, true, "together"},
		{"cwd", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--x"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "home", "output_name": "out.txt", "output": validOutput}}, true, "review.cwd"},
		{"home-policy", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--x"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "home_policy": "maybe", "output_name": "out.txt", "output": validOutput}}, true, "review.home_policy"},
		{"stderr", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--x"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "output_name": "out.txt", "output": map[string]any{"source": "stderr", "parse": "raw"}}}, true, "stderr"},
		{"empty-arg", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{""}}, "session": map[string]any{"mode": "none"}, "review": validReview}, true, "args.review"},
		{"unknown-placeholder", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"{shell}"}}, "session": map[string]any{"mode": "none"}, "review": validReview}, true, "placeholder"},
		{"stdin-none-missing", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--x"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "output_name": "out.txt", "output": validOutput, "stdin": "none"}}, true, "instruction"},
		{"stdin-dup", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"{instruction}"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "output_name": "out.txt", "output": validOutput, "stdin": "instruction"}}, true, "instruction"},
		{"prompt-traverse", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--file", "{prompt_file:guide}"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "output_name": "out.txt", "output": validOutput, "prompt_files": []any{map[string]any{"name": "guide", "path": "../x", "template": "{prompt}"}}}}, true, "path"},
		{"prompt-unknown-ref", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"{prompt_file:missing}"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "output_name": "out.txt", "output": validOutput}}, true, "prompt"},
		{"ok", map[string]any{"path": executable, "args": map[string]any{"start": []string{}, "resume": []string{}, "review": []string{"--file", "{prompt_file:guide}", "{instruction}"}}, "session": map[string]any{"mode": "none"}, "review": map[string]any{"cwd": "runtime", "home_policy": "optional", "output_name": "out.txt", "stdin": "none", "snapshot_spec": true, "spawns_helpers": false, "output": validOutput, "prompt_files": []any{map[string]any{"name": "guide", "path": "guide.md", "template": "{inspection}\n{prompt}"}}}}, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var root map[string]any
			json.Unmarshal(base, &root)
			root["agents"] = map[string]any{"helper": test.def}
			data, _ := json.Marshal(root)
			_, err := ValidateJSON(data)
			if (err != nil) != test.bad {
				t.Fatalf("err=%v", err)
			}
			if test.bad && err != nil {
				msg := err.Error()
				if !strings.Contains(msg, "helper") || !strings.Contains(msg, test.want) {
					t.Fatalf("error %q missing helper or %q", msg, test.want)
				}
			}
		})
	}
}

func TestStartOnlyReviewerRejectedAndDoctorLeavesIt(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]AgentDefinition{
		"plain": {Path: exe, Args: &AgentArgs{Start: []string{"--go"}, Resume: []string{}}, Session: &AgentSessionDefinition{Mode: "generated"}},
	}
	cfg.Reviewers["PM"] = "plain"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateJSON(data); err == nil || !strings.Contains(err.Error(), "args.review") {
		t.Fatalf("validate: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	_, _, err = Repair(nil)
	if err == nil || !strings.Contains(err.Error(), "args.review") {
		t.Fatalf("repair: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("doctor rewrote start-only reviewer")
	}
}

func TestReviewAgentNamesFollowsDefinitions(t *testing.T) {
	if !HasReviewTemplate(nil, "codex") || !contains(ReviewAgentNames(nil), "cursor") {
		t.Fatal(ReviewAgentNames(nil))
	}
	cfg := DefaultConfig()
	cfg.Agents = map[string]AgentDefinition{
		"plain":    {Args: &AgentArgs{Start: []string{}, Resume: []string{}}, Session: &AgentSessionDefinition{Mode: "none"}},
		"helper":   {Path: "/bin/helper", Dialect: "claude"},
		"wrap":     {Dialect: "claude"},
		"declared": {Dialect: "claude", Args: &AgentArgs{Start: []string{}, Resume: []string{}, Review: []string{"--x"}}},
		"reviewer": {
			Args:    &AgentArgs{Start: []string{}, Resume: []string{}, Review: []string{"--x"}},
			Session: &AgentSessionDefinition{Mode: "none"},
			Review:  &AgentReview{},
		},
		"claude": {Path: "/bin/wrapper"},
	}
	if HasReviewTemplate(cfg, "plain") {
		t.Fatal("start-only")
	}
	if HasReviewTemplate(cfg, "helper") || contains(ReviewAgentNames(cfg), "helper") {
		t.Fatal("dialect wrapper")
	}
	if HasReviewTemplate(cfg, "wrap") || contains(ReviewAgentNames(cfg), "wrap") {
		t.Fatal("undeclared custom reviewer")
	}
	if !HasReviewTemplate(cfg, "declared") || !contains(ReviewAgentNames(cfg), "declared") {
		t.Fatal("declared overlay")
	}
	if !HasReviewTemplate(cfg, "reviewer") {
		t.Fatal("declared review")
	}
	if !HasReviewTemplate(cfg, "claude") {
		t.Fatal("builtin overlay")
	}
	resolved := AgentFor(cfg, "wrap")
	if resolved.Args != nil && resolved.Args.Review != nil || resolved.Review != nil {
		t.Fatal("dialect wrapper inherited review")
	}
	pair := AgentFor(cfg, "reviewer")
	if pair.Review == nil {
		t.Fatal("declared pair")
	}
	if pair.Review.CWD != "" || pair.Review.Output != nil || pair.Review.OutputName != "" {
		t.Fatal("declared pair should not inherit omitted review fields")
	}
	paired := AgentDefinition{
		Dialect: "claude",
		Args:    &AgentArgs{Start: []string{}, Resume: []string{}, Review: []string{"--x"}},
		Review:  &AgentReview{CWD: ReviewCWDRoot, OutputName: "out.md"},
	}
	filled := AgentFor(&Config{Agents: map[string]AgentDefinition{"paired": paired}}, "paired")
	if filled.Review == nil || filled.Review.Env != nil || filled.Review.Inspection != "" || filled.Review.HomeEnv != "" {
		t.Fatal("declared pair must not fill omitted review fields")
	}
}
