package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// customAgent is a valid custom agent entry, the base the unknown-key cases extend.
func customAgent() map[string]any {
	return map[string]any{"path": "sh", "dialect": "claude", "session": map[string]any{"mode": "generated"}}
}

func ignoredPaths(cfg *Config) []string {
	var out []string
	for _, item := range cfg.Ignored {
		out = append(out, item.PathString())
	}
	slices.Sort(out)
	return out
}

func scopeDocument(t *testing.T) map[string]any {
	t.Helper()
	raw := defaultRaw(t)
	raw["welcome_complete"] = true
	return raw
}

func writeScope(t *testing.T, doc map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	writeJSONFile(t, path, doc)
	return path
}

func readScope(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw.(map[string]any)
}

func jsonPath(doc map[string]any, path ...string) (any, bool) { return OverlayGet(doc, path...) }

// unknownCases injects one unknown key at every level the schema has.
var unknownCases = []struct {
	path  []string
	value any
}{
	{[]string{"future_top"}, map[string]any{"enabled": true}},
	{[]string{"kanban_agents", "future_scale"}, "codex"},
	{[]string{"tui", "future"}, 1},
	{[]string{"rules", "future_module"}, true},
	{[]string{"git", "future"}, "x"},
	{[]string{"issue", "future"}, []any{"a"}},
	{[]string{"reviewers", "future_scale"}, map[string]any{"PMQA": "codex"}},
	{[]string{"reviewers", "large", "FutureRole"}, "codex"},
	{[]string{"review_stages", "future_scale"}, map[string]any{"PMQA": "auto"}},
	{[]string{"review_stages", "small", "FutureRole"}, "auto"},
	{[]string{"models", "future_section"}, map[string]any{"a": "b"}},
	{[]string{"models", "kanban", "future_agent"}, map[string]any{"large_model": "m"}},
	{[]string{"models", "kanban", "codex", "future_field"}, "x"},
	{[]string{"models", "review", "future_agent"}, map[string]any{"model": "m"}},
	{[]string{"models", "review", "codex", "future_field"}, "x"},
	{[]string{"models", "chat", "future_agent"}, map[string]any{"model": "m"}},
	{[]string{"models", "chat", "codex", "future_field"}, "x"},
	{[]string{"models", "chat", "cursor", "effort"}, "high"},
	{[]string{"models", "review_roles", "FutureRole"}, map[string]any{"model": "m"}},
	{[]string{"models", "review_roles", "PMQA", "future_field"}, "x"},
	{[]string{"agents", "custom", "future_key"}, map[string]any{"a": 1}},
	{[]string{"agents", "custom", "session", "future_session_key"}, "x"},
}

func documentWithUnknowns(t *testing.T) map[string]any {
	t.Helper()
	doc := scopeDocument(t)
	doc["agents"] = map[string]any{"custom": customAgent()}
	for _, c := range unknownCases {
		OverlaySet(doc, c.value, c.path...)
	}
	return doc
}

func TestUnknownKeysAtEveryLevelAreIgnoredAndListed(t *testing.T) {
	setupHome(t)
	path := writeScope(t, documentWithUnknowns(t))
	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("unknown keys must not fail the load: %v", err)
	}
	var want []string
	for _, c := range unknownCases {
		want = append(want, strings.Join(c.path, "."))
	}
	slices.Sort(want)
	if got := ignoredPaths(cfg); !slices.Equal(got, want) {
		t.Fatalf("ignored=%v\nwant   %v", got, want)
	}
	// The unknown keys have no effect: the settings equal those of the same file without them.
	clean := scopeDocument(t)
	clean["agents"] = map[string]any{"custom": customAgent()}
	writeJSONFile(t, path, clean)
	base, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Ignored, cfg.UnusableAgents = nil, nil
	if !reflect.DeepEqual(cfg, base) {
		t.Fatalf("unknown keys changed the settings:\n got %+v\nwant %+v", cfg, base)
	}
}

func TestSaveKeepsUnknownKeysAtEveryLevel(t *testing.T) {
	setupHome(t)
	path := writeScope(t, documentWithUnknowns(t))
	for name, save := range map[string]func(*Config) error{
		"Save": func(cfg *Config) error { _, err := Save(cfg); return err },
		"SaveIfUnchanged": func(cfg *Config) error {
			_, err := SaveIfUnchanged(cfg, Clone(cfg))
			return err
		},
		"Update": func(cfg *Config) error {
			_, err := Update(func(c *Config) error { c.TUI.Columns = 3; return nil })
			return err
		},
		"Repair": func(*Config) error { _, _, err := Repair(nil); return err },
	} {
		t.Run(name, func(t *testing.T) {
			writeJSONFile(t, path, documentWithUnknowns(t))
			cfg, err := Load(false)
			if err != nil {
				t.Fatal(err)
			}
			if err := save(cfg); err != nil {
				t.Fatal(err)
			}
			saved := readScope(t, path)
			for _, c := range unknownCases {
				got, ok := jsonPath(saved, c.path...)
				if !ok {
					t.Fatalf("%s lost after save", strings.Join(c.path, "."))
				}
				want, _ := json.Marshal(c.value)
				have, _ := json.Marshal(got)
				if string(want) != string(have) {
					t.Fatalf("%s changed: %s != %s", strings.Join(c.path, "."), have, want)
				}
			}
			reloaded, err := Load(false)
			if err != nil {
				t.Fatalf("saved file does not load: %v", err)
			}
			if len(reloaded.Ignored) != len(unknownCases) {
				t.Fatalf("ignored after save=%v", ignoredPaths(reloaded))
			}
		})
	}
}

func TestSaveWithoutUnknownKeysKeepsTheEncodedFormat(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	want, err := encodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("a config without unknown keys is no longer written as before: %v", err)
	}
}

func TestSaveKeepsKnownKeyOrderWhenAppendingUnknownKeys(t *testing.T) {
	setupHome(t)
	path := writeScope(t, documentWithUnknowns(t))
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Index(text, `"schema_version"`) > strings.Index(text, `"welcome_complete"`) ||
		strings.Index(text, `"welcome_complete"`) > strings.Index(text, `"kanban_agent"`) ||
		strings.Index(text, `"future_top"`) < strings.Index(text, `"install_rules"`) {
		t.Fatalf("known keys must keep their order and unknown keys follow them:\n%s", text)
	}
}

func TestLegacyKeysCountAsKnown(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["reviewers"] = map[string]any{"PM": "codex", "QA": "codex", "CSA": "codex", "Hacker": "codex"}
	doc["review_stages"] = map[string]any{"PM": "required", "QA": "skip", "CSA": "auto", "Hacker": "auto"}
	models := doc["models"].(map[string]any)
	models["review_roles"] = map[string]any{"PM": map[string]any{"model": "m"}, "Hacker": map[string]any{"effort": "high"}}
	models["kanban"].(map[string]any)["codex"].(map[string]any)["model"] = "legacy"
	path := writeScope(t, doc)
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ignoredPaths(cfg); len(got) != 0 {
		t.Fatalf("legacy keys must not be listed as ignored: %v", got)
	}
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved := readScope(t, path)
	for _, legacy := range [][]string{{"reviewers", "PM"}, {"review_stages", "QA"}, {"models", "review_roles", "PM"}} {
		if _, ok := jsonPath(saved, legacy...); ok {
			t.Fatalf("legacy key %v survived the save", legacy)
		}
	}
}

func TestUnknownAgentReferencesLoadAndFailOnUse(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["kanban_agent"] = "newagent"
	doc["kanban_agents"] = map[string]any{"large": "newagent", "small": "codex"}
	doc["chat_agent"] = "newchat"
	doc["reviewers"] = map[string]any{"large": map[string]any{"PMQA": "newreviewer", "Security": "codex"}, "small": map[string]any{"PMQA": "codex", "Security": "codex"}}
	OverlaySet(doc, map[string]any{"large_model": "m"}, "models", "kanban", "newagent")
	path := writeScope(t, doc)
	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("an unknown agent reference must not fail the load: %v", err)
	}
	var got []string
	for _, ref := range UnknownAgentRefs(cfg) {
		got = append(got, ref.Field+"="+ref.Agent)
	}
	want := []string{"kanban_agent=newagent", "kanban_agents.large=newagent", "chat_agent=newchat", "reviewers.large.PMQA=newreviewer"}
	if !slices.Equal(got, want) {
		t.Fatalf("refs=%v want %v", got, want)
	}
	err = UnavailableAgentError(cfg, "newagent")
	if err == nil || err.Error() != Text("config.agent_unknown_reference", "newagent") {
		t.Fatalf("err=%v", err)
	}
	if KanbanAgentOrError(t, cfg, "large") != "newagent" || cfg.Models.Kanban["newagent"] != nil {
		t.Fatalf("the reference must stay as written without inventing models: %v", cfg.Models.Kanban["newagent"])
	}
	if cfg.Models.Chat["newchat"] != nil {
		t.Fatalf("a chat entry was invented for an unknown agent: %v", cfg.Models.Chat["newchat"])
	}
	// A malformed name is still an error: it cannot come from a newer version.
	doc["kanban_agent"] = "Not A Name"
	writeJSONFile(t, path, doc)
	if _, err := Load(false); err == nil {
		t.Fatal("a malformed agent name was accepted")
	}
}

func KanbanAgentOrError(t *testing.T, cfg *Config, scale string) string {
	t.Helper()
	agent, err := KanbanAgentFor(cfg, scale)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestSaveAndRepairKeepUnknownAgentReferences(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["kanban_agent"] = "newagent"
	doc["kanban_agents"] = map[string]any{"large": "newagent", "small": "newagent"}
	doc["chat_agent"] = "newagent"
	doc["reviewers"] = map[string]any{"large": map[string]any{"PMQA": "newagent", "Security": "newagent"}, "small": map[string]any{"PMQA": "newagent", "Security": "newagent"}}
	OverlaySet(doc, map[string]any{"large_model": "m", "future": true}, "models", "kanban", "newagent")
	OverlaySet(doc, map[string]any{"model": "m"}, "models", "chat", "newagent")
	path := writeScope(t, doc)
	check := func(step string) {
		t.Helper()
		saved := readScope(t, path)
		for _, p := range [][]string{
			{"kanban_agent"}, {"kanban_agents", "large"}, {"kanban_agents", "small"}, {"chat_agent"},
			{"reviewers", "large", "PMQA"}, {"reviewers", "small", "Security"},
		} {
			if v, _ := jsonPath(saved, p...); v != "newagent" {
				t.Fatalf("%s changed %v to %v", step, p, v)
			}
		}
		for _, p := range [][]string{{"models", "kanban", "newagent"}, {"models", "chat", "newagent"}} {
			if _, ok := jsonPath(saved, p...); !ok {
				t.Fatalf("%s lost %v", step, p)
			}
		}
		if v, _ := jsonPath(saved, "models", "kanban", "newagent", "large_model"); v != "m" {
			t.Fatalf("%s changed the entry of the unknown agent: %v", step, v)
		}
	}
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	check("Save")
	// A broken known value makes Repair rewrite the file; the references stay.
	saved := readScope(t, path)
	OverlaySet(saved, 99, "tui", "columns")
	writeJSONFile(t, path, saved)
	repaired, result, err := Repair(nil)
	if err != nil || !result.Changed {
		t.Fatalf("repair=%+v err=%v", result, err)
	}
	if repaired.TUI.Columns != DefaultTUIColumns {
		t.Fatalf("repair did not fix the known value: %d", repaired.TUI.Columns)
	}
	check("Repair")
}

func TestUnusableAgentEntriesDegradeOneAgent(t *testing.T) {
	setupHome(t)
	for _, tc := range []struct {
		name   string
		entry  map[string]any
		reason string
	}{
		{"install_rules on a custom name", map[string]any{"path": "sh", "dialect": "claude", "install_rules": false}, Text("config.agent_install_rules_builtin_only")},
		{"config_dir on a custom name", map[string]any{"path": "sh", "dialect": "claude", "config_dir": "/tmp/x"}, Text("config.agent_config_dir_builtin_only")},
		{"no dialect and no args", map[string]any{"path": "sh"}, Text("config.agent_adapter")},
		{"unknown dialect", map[string]any{"path": "sh", "dialect": "futuredialect"}, Text("config.agent_dialect")},
		{"unregistered hook", map[string]any{"path": "sh", "args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "hook:future-hook"}}, Text("config.agent_unusable_session_mode", "hook:future-hook")},
		{"unregistered mode", map[string]any{"path": "sh", "args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "future-mode"}}, Text("config.agent_unusable_session_mode", "future-mode")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := scopeDocument(t)
			doc["agents"] = map[string]any{"agy": tc.entry, "custom": customAgent()}
			doc["kanban_agent"] = "agy"
			path := writeScope(t, doc)
			cfg, err := Load(false)
			if err != nil {
				t.Fatalf("an unusable agents entry must not fail the load: %v", err)
			}
			if HasAgent(cfg, "agy") || cfg.UnusableAgents["agy"] != tc.reason {
				t.Fatalf("agy must be unusable with %q: has=%v %v", tc.reason, HasAgent(cfg, "agy"), cfg.UnusableAgents)
			}
			if !HasAgent(cfg, "custom") || !HasAgent(cfg, "codex") {
				t.Fatal("the rest of the configuration must stay usable")
			}
			err = UnavailableAgentError(cfg, "agy")
			if err == nil || !strings.Contains(err.Error(), "agy") || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("the error must name the agent and the reason: %v", err)
			}
			var kinds []string
			for _, item := range cfg.Ignored {
				if item.Kind == IgnoredUnusableAgent {
					kinds = append(kinds, item.PathString())
				}
			}
			if !slices.Equal(kinds, []string{"agents.agy"}) {
				t.Fatalf("unusable entries=%v", kinds)
			}
			// Save and Repair keep the entry whole.
			if _, err := Save(cfg); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Repair(nil); err != nil {
				t.Fatal(err)
			}
			saved := readScope(t, path)
			got, _ := jsonPath(saved, "agents", "agy")
			want, _ := json.Marshal(tc.entry)
			have, _ := json.Marshal(got)
			if string(want) != string(have) {
				t.Fatalf("the unusable entry changed: %s != %s", have, want)
			}
			if v, _ := jsonPath(saved, "kanban_agent"); v != "agy" {
				t.Fatalf("the reference to the unusable agent changed: %v", v)
			}
		})
	}
}

func TestUnusableBuiltinEntryTakesTheBuiltinOutOfService(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["agents"] = map[string]any{"codex": map[string]any{"session": map[string]any{"mode": "hook:future-hook"}, "args": map[string]any{"start": []any{}, "resume": []any{}}}}
	writeScope(t, doc)
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if HasAgent(cfg, "codex") || !HasAgent(cfg, "claude") {
		t.Fatalf("only the agent with the unusable entry may be out of service: %v", AgentNames(cfg))
	}
}

func TestInvalidKnownAgentValuesStillFail(t *testing.T) {
	setupHome(t)
	for name, entry := range map[string]map[string]any{
		"control character in path": {"path": "bad\tvalue", "dialect": "claude"},
		"bad install_rules type":    {"install_rules": "no"},
		"bad args template":         {"path": "sh", "args": map[string]any{"start": []any{"{prompt}"}, "resume": []any{}}, "session": map[string]any{"mode": "generated"}},
	} {
		doc := scopeDocument(t)
		agent := "codex"
		doc["agents"] = map[string]any{agent: entry}
		writeScope(t, doc)
		if _, err := Load(false); err == nil {
			t.Fatalf("%s: an invalid known value was accepted", name)
		}
	}
}

func TestRepairLeavesAValidFileWithUnknownContentAlone(t *testing.T) {
	setupHome(t)
	path := writeScope(t, documentWithUnknowns(t))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Normalize once so the comparison below is about unknown content only.
	if _, _, err := Repair(nil); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(path)
	_, result, err := Repair(nil)
	if err != nil || result.Changed {
		t.Fatalf("repair rewrote a valid file: %+v %v", result, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("repair changed a valid file")
	}
}

func TestOverlayUnknownKeysMergeAndSurviveASave(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scopePath := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scopePath)
	writeJSONFile(t, scopePath, scopeDocument(t))
	overlayPath := filepath.Join(main, OverlayFilename)
	overlay := map[string]any{
		"tui":          map[string]any{"refresh": 45, "future": true},
		"future_top":   map[string]any{"x": 1},
		"kanban_agent": "newagent",
		"models":       map[string]any{"kanban": map[string]any{"newagent": map[string]any{"large_model": "m"}}},
	}
	writeJSONFile(t, overlayPath, overlay)
	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("unknown overlay keys must not fail the merge: %v", err)
	}
	if cfg.TUI.Refresh != 45 {
		t.Fatalf("known overlay values stopped applying: %d", cfg.TUI.Refresh)
	}
	sources := map[string]string{}
	for _, item := range cfg.Ignored {
		sources[item.PathString()] = item.Source
	}
	for _, p := range []string{"future_top", "tui.future", "models.kanban.newagent"} {
		if sources[p] != overlayPath {
			t.Fatalf("%s must name the overlay as its source: %v", p, sources)
		}
	}
	// An overlay save keeps the unknown keys.
	baseline, err := ReadOverlayFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := CloneOverlay(baseline)
	OverlaySet(edited, 60, "tui", "refresh")
	if _, err := SaveOverlayIfUnchanged(overlayPath, edited, baseline); err != nil {
		t.Fatalf("an overlay with unknown keys must be saveable: %v", err)
	}
	saved := readScope(t, overlayPath)
	for _, p := range [][]string{{"future_top", "x"}, {"tui", "future"}, {"kanban_agent"}, {"models", "kanban", "newagent", "large_model"}} {
		if _, ok := jsonPath(saved, p...); !ok {
			t.Fatalf("overlay save lost %v", p)
		}
	}
	// The scope file never receives overlay content.
	scope, err := LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(scope); err != nil {
		t.Fatal(err)
	}
	scopeSaved := readScope(t, scopePath)
	for _, p := range [][]string{{"future_top"}, {"tui", "future"}} {
		if _, ok := jsonPath(scopeSaved, p...); ok {
			t.Fatalf("overlay content %v leaked into the scope file", p)
		}
	}
}

func TestOverlayForbiddenKeysStillFail(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	writeScopeFile(t, filepath.Join(root, "config.json"))
	writeJSONFile(t, filepath.Join(main, OverlayFilename), map[string]any{"schema_version": 1, "future_top": true})
	if _, err := Load(true); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("a forbidden key must still fail next to unknown keys: %v", err)
	}
}

func TestStrictPlacesStayStrict(t *testing.T) {
	// The embedded definitions stay strict: take a valid one and add a single unknown key.
	data, err := embeddedAgentFS.ReadFile("agents/codex.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseEmbeddedAgentFile("codex.json", data); err != nil {
		t.Fatalf("the unmodified embedded definition must parse: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["future_key"] = true
	data, _ = json.Marshal(doc)
	if _, err := parseEmbeddedAgentFile("codex.json", data); err == nil || !strings.Contains(err.Error(), "future_key") {
		t.Fatalf("an embedded definition accepted an unknown key: %v", err)
	}
}

// A built-in agent whose entry this version cannot use keeps the values the file holds
// for it, and keeps its install policy.
func TestUnusableBuiltinKeepsItsModelsAndPolicy(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["agents"] = map[string]any{"codex": map[string]any{
		"install_rules": false, "config_dir": "/tmp/codex-home",
		"args":    map[string]any{"start": []any{}, "resume": []any{}},
		"session": map[string]any{"mode": "hook:future-hook"},
	}}
	OverlaySet(doc, "user-model", "models", "kanban", "codex", "large_model")
	OverlaySet(doc, "user-review", "models", "review", "codex", "model")
	OverlaySet(doc, "user-chat", "models", "chat", "codex", "model")
	path := writeScope(t, doc)
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if HasAgent(cfg, "codex") || HasReviewTemplate(cfg, "codex") {
		t.Fatal("an unusable built-in agent must be out of service as executor and reviewer")
	}
	if InstallRulesOf(cfg).Enabled("codex") {
		t.Fatal("install_rules=false of an unusable entry must keep applying")
	}
	paths, err := CurrentInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	if dir, err := ScopeAgentConfigDir(paths, "codex"); err != nil || dir != "/tmp/codex-home" {
		t.Fatalf("config_dir of an unusable entry must keep applying: %q %v", dir, err)
	}
	// Options-style switch rewriting must not turn the entry into a different one.
	SetAgentInstallRules(cfg, "codex", nil)
	ApplyAgentInstallRules(cfg, InstallRulesOf(cfg))
	cfg.TUI.Columns = 3
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved := readScope(t, path)
	for _, p := range [][]string{{"models", "kanban", "codex", "large_model"}, {"models", "review", "codex", "model"}, {"models", "chat", "codex", "model"}} {
		got, _ := jsonPath(saved, p...)
		if !strings.HasPrefix(got.(string), "user-") {
			t.Fatalf("%v was replaced by a default: %v", p, got)
		}
	}
	if mode, _ := jsonPath(saved, "agents", "codex", "session", "mode"); mode != "hook:future-hook" {
		t.Fatalf("the unusable entry changed: %v", mode)
	}
	// Repair rewrites a file with a broken known value and keeps the same content.
	OverlaySet(saved, 99, "tui", "columns")
	writeJSONFile(t, path, saved)
	if _, result, err := Repair(nil); err != nil || !result.Changed {
		t.Fatalf("repair=%+v err=%v", result, err)
	}
	repaired := readScope(t, path)
	if got, _ := jsonPath(repaired, "models", "kanban", "codex", "large_model"); got != "user-model" {
		t.Fatalf("repair replaced the models of an unusable built-in agent: %v", got)
	}
	if mode, _ := jsonPath(repaired, "agents", "codex", "session", "mode"); mode != "hook:future-hook" {
		t.Fatalf("repair changed the unusable entry: %v", mode)
	}
}

// Keys that differ from a declared key only in case are unknown, not the declared key.
func TestAgentKeysMatchExactly(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	doc["agents"] = map[string]any{"codex": map[string]any{"Path": "/opt/evil/codex"}}
	writeScope(t, doc)
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if got := AgentPath(cfg, "codex"); got != "codex" {
		t.Fatalf("a key spelled with another case changed the program: %q", got)
	}
	if got := ignoredPaths(cfg); !slices.Equal(got, []string{"agents.codex.Path"}) {
		t.Fatalf("ignored=%v", got)
	}
}

func TestKnownValuesStayCheckedBesideADowngradeReason(t *testing.T) {
	setupHome(t)
	for name, entry := range map[string]map[string]any{
		"bad path beside install_rules":      {"path": "bad\tvalue", "dialect": "claude", "install_rules": false},
		"bad path beside unknown dialect":    {"path": "bad\tvalue", "dialect": "futuredialect"},
		"bad config_dir beside session mode": {"config_dir": "relative", "args": map[string]any{"start": []any{}, "resume": []any{}}, "session": map[string]any{"mode": "future-mode"}},
	} {
		doc := scopeDocument(t)
		doc["agents"] = map[string]any{"codex": entry}
		if name == "bad path beside install_rules" || name == "bad path beside unknown dialect" {
			doc["agents"] = map[string]any{"custom": entry}
		}
		writeScope(t, doc)
		if _, err := Load(false); err == nil {
			t.Fatalf("%s: an invalid known value was hidden by a downgrade reason", name)
		}
	}
}

func TestReviewRoleExtrasSurviveTheFold(t *testing.T) {
	setupHome(t)
	doc := scopeDocument(t)
	OverlaySet(doc, map[string]any{"model": "m", "future": map[string]any{"a": 1}}, "models", "review_roles", "PMQA")
	path := writeScope(t, doc)
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ignoredPaths(cfg); !slices.Equal(got, []string{"models.review_roles.PMQA.future"}) {
		t.Fatalf("ignored=%v", got)
	}
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := jsonPath(readScope(t, path), "models", "review_roles", "PMQA", "future", "a"); !ok {
		t.Fatal("an unknown field of a current role was lost on save")
	}
	// An unknown field of a legacy role is kept under the current role it folds into.
	legacy := scopeDocument(t)
	OverlaySet(legacy, map[string]any{"model": "m", "future": "x"}, "models", "review_roles", "PM")
	writeJSONFile(t, path, legacy)
	cfg, err = Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if v, _ := jsonPath(readScope(t, path), "models", "review_roles", "PMQA", "future"); v != "x" {
		t.Fatalf("an unknown field of a legacy role was lost on save: %v", v)
	}
	// The fold used by the overlay merge and the overlay write keeps it too.
	raw := map[string]any{"models": map[string]any{"review_roles": map[string]any{
		"PM": map[string]any{"model": "m", "future": "x"},
	}}}
	FoldLegacyReviewRoleKeys(raw)
	if v, _ := jsonPath(raw, "models", "review_roles", "PMQA", "future"); v != "x" {
		t.Fatalf("an unknown field of a legacy role must move to the current role: %v", raw)
	}
}

func TestFlatLegacyOverlayKeepsUnknownRolePath(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scopePath := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scopePath)
	writeJSONFile(t, scopePath, scopeDocument(t))
	overlayPath := filepath.Join(main, OverlayFilename)
	writeJSONFile(t, overlayPath, map[string]any{"reviewers": map[string]any{"PMQA": "codex", "FutureRole": "codex"}})
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ignoredPaths(cfg); !slices.Equal(got, []string{"reviewers.FutureRole"}) {
		t.Fatalf("the ignored path must be the one in the file: %v", got)
	}
	if cfg.Ignored[0].Source != overlayPath {
		t.Fatalf("source=%q", cfg.Ignored[0].Source)
	}
}

func TestOverlayWithOnlyUnknownTUIKeysOverAScopeWithoutTUI(t *testing.T) {
	setupHome(t)
	root := t.TempDir()
	main := initGitRepo(t, filepath.Join(root, "repo"))
	t.Chdir(main)
	scopePath := filepath.Join(root, "config.json")
	t.Setenv(EnvConfig, scopePath)
	scope := scopeDocument(t)
	delete(scope, "tui")
	writeJSONFile(t, scopePath, scope)
	writeJSONFile(t, filepath.Join(main, OverlayFilename), map[string]any{"tui": map[string]any{"future": true}})
	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("an overlay section of unknown keys must not fail the merge: %v", err)
	}
	if cfg.TUI != DefaultTUI() || !slices.Equal(ignoredPaths(cfg), []string{"tui.future"}) {
		t.Fatalf("tui=%+v ignored=%v", cfg.TUI, ignoredPaths(cfg))
	}
}
