package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func validPolicyDocument() map[string]any {
	return map[string]any{
		"git":           map[string]any{"branch_mode": "main-only"},
		"review_stages": map[string]any{"large": map[string]any{"PMQA": "required", "Security": "skip"}, "small": map[string]any{"PMQA": "auto", "Security": "auto"}},
		"rules": map[string]any{
			"code": true, "collaboration": true, "git": true, "reporting": false, "review": true, "task_groups": true, "task_intake": true,
		},
		"agent_language": "zh-CN",
		"issue":          map[string]any{"result_on_done": "ask"},
	}
}

func policyJSON(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func policyProblems(t *testing.T, err error) *PolicyProblems {
	t.Helper()
	var problems *PolicyProblems
	if !errors.As(err, &problems) {
		t.Fatalf("error %v is not a *PolicyProblems", err)
	}
	return problems
}

func TestValidatePolicyValuesAcceptsTheFiveValues(t *testing.T) {
	values, err := ValidatePolicyValues(policyJSON(t, validPolicyDocument()))
	if err != nil {
		t.Fatal(err)
	}
	if values.Git.BranchMode != "main-only" || values.ReviewStages["large"]["PMQA"] != "required" || values.Rules[RuleReporting] || values.AgentLanguage != "zh-CN" || values.Issue.ResultOnDone != "ask" {
		t.Fatalf("values = %+v", values)
	}
}

func TestValidatePolicyValuesListsEveryMissingValue(t *testing.T) {
	for _, key := range PolicyKeys {
		doc := validPolicyDocument()
		delete(doc, key)
		problems := policyProblems(t, func() error { _, err := ValidatePolicyValues(policyJSON(t, doc)); return err }())
		if !slices.Equal(problems.Missing, []string{key}) {
			t.Errorf("without %s: missing %v", key, problems.Missing)
		}
	}
	doc := validPolicyDocument()
	doc["git"] = map[string]any{}
	doc["review_stages"].(map[string]any)["small"] = map[string]any{"PMQA": "auto"}
	problems := policyProblems(t, func() error { _, err := ValidatePolicyValues(policyJSON(t, doc)); return err }())
	if !slices.Equal(problems.Missing, []string{"git.branch_mode", "review_stages.small.Security"}) {
		t.Fatalf("missing %v", problems.Missing)
	}
}

func TestValidatePolicyValuesRefusesAndListsUnknownKeys(t *testing.T) {
	doc := validPolicyDocument()
	doc["launcher"] = "tmux"
	doc["git"].(map[string]any)["future"] = true
	doc["issue"].(map[string]any)["future"] = true
	doc["rules"].(map[string]any)["future_module"] = true
	doc["review_stages"].(map[string]any)["medium"] = map[string]any{"PMQA": "auto"}
	doc["review_stages"].(map[string]any)["large"].(map[string]any)["Design"] = "auto"
	_, err := ValidatePolicyValues(policyJSON(t, doc))
	problems := policyProblems(t, err)
	want := []string{"launcher", "git.future", "issue.future", "review_stages.medium", "review_stages.large.Design", "rules.future_module"}
	if !slices.Equal(problems.Unknown, want) {
		t.Fatalf("unknown %v, want %v", problems.Unknown, want)
	}
	for _, key := range want {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("message %q does not name %s", err.Error(), key)
		}
	}
}

func TestValidatePolicyValuesRefusesInvalidValues(t *testing.T) {
	cases := map[string]func(map[string]any){
		"branch mode": func(d map[string]any) { d["git"] = map[string]any{"branch_mode": "trunk"} },
		"stage mode": func(d map[string]any) {
			d["review_stages"].(map[string]any)["small"].(map[string]any)["PMQA"] = "always"
		},
		"stage structure":  func(d map[string]any) { d["review_stages"] = "auto" },
		"mixed stages":     func(d map[string]any) { d["review_stages"] = map[string]any{"large": map[string]any{}, "PMQA": "auto"} },
		"rule value":       func(d map[string]any) { d["rules"].(map[string]any)["code"] = "yes" },
		"rule dependency":  func(d map[string]any) { d["rules"].(map[string]any)["git"] = false },
		"agent language":   func(d map[string]any) { d["agent_language"] = "" },
		"result on done":   func(d map[string]any) { d["issue"] = map[string]any{"result_on_done": "always"} },
		"section is array": func(d map[string]any) { d["issue"] = []any{"auto"} },
	}
	for name, mutate := range cases {
		doc := validPolicyDocument()
		mutate(doc)
		_, err := ValidatePolicyValues(policyJSON(t, doc))
		if problems := policyProblems(t, err); len(problems.Invalid) == 0 {
			t.Errorf("%s: no invalid value reported: %+v", name, problems)
		}
	}
	if _, err := ValidatePolicyValues([]byte(`["git"]`)); err == nil {
		t.Fatal("an array document was accepted")
	}
}

func TestValidatePolicyValuesNormalizesStagesAndFillsMissingModules(t *testing.T) {
	doc := validPolicyDocument()
	doc["review_stages"] = map[string]any{"PMQA": "required", "Security": "skip"}
	doc["rules"] = map[string]any{"review": false}
	values, err := ValidatePolicyValues(policyJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range TaskScales {
		if values.ReviewStages[scale]["PMQA"] != "required" || values.ReviewStages[scale]["Security"] != "skip" {
			t.Fatalf("stages %v", values.ReviewStages)
		}
	}
	for _, module := range RuleModules {
		if values.Rules[module] != (module != RuleReview) {
			t.Fatalf("rules %v", values.Rules)
		}
	}
}

func TestReadPolicyValuesIgnoresUnknownKeysButNotMissingValues(t *testing.T) {
	doc := validPolicyDocument()
	doc["rules"].(map[string]any)["future_module"] = true
	doc["future_section"] = map[string]any{"a": 1}
	values, ignored, err := ReadPolicyValues(policyJSON(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ignored, []string{"future_section", "rules.future_module"}) || values.Git.BranchMode != "main-only" {
		t.Fatalf("ignored %v values %+v", ignored, values)
	}
	delete(doc, "agent_language")
	if _, _, err := ReadPolicyValues(policyJSON(t, doc)); err == nil {
		t.Fatal("a missing value was accepted")
	}
}

// isolatePolicyClient gives a test its own scope config, client home and working directory, and
// returns the client home.
func isolatePolicyClient(t *testing.T, scope map[string]any) (home, scopePath string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "client")
	t.Setenv(EnvClientHome, home)
	scopePath = filepath.Join(root, "scope", "config.json")
	t.Setenv(EnvConfig, scopePath)
	doc := map[string]any{
		"schema_version": SchemaVersion, "welcome_complete": true, "kanban_agent": "codex", "launcher": "auto",
		"reviewers": map[string]any{}, "server": map[string]any{"url": "http://Example.test:7878/"}, "project": map[string]any{"name": "golf"},
	}
	for key, value := range scope {
		doc[key] = value
	}
	writeJSONFile(t, scopePath, doc)
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	return home, scopePath
}

func storePolicy(t *testing.T, home string, snapshot PolicySnapshot) bool {
	t.Helper()
	stored, err := StorePolicy(home, "http://example.test:7878", ProjectIDForName("golf"), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func policySnapshot(t *testing.T, epoch string, revision uint64, doc map[string]any) PolicySnapshot {
	return PolicySnapshot{Epoch: epoch, Revision: revision, Values: policyJSON(t, doc), UpdatedAt: time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC), UpdatedBy: "dev"}
}

func TestLoadAppliesTheCachedPolicyOverTheFilesAndListsReplacedValues(t *testing.T) {
	home, scopePath := isolatePolicyClient(t, map[string]any{
		"git":           map[string]any{"branch_mode": "main-develop"},
		"review_stages": map[string]any{"large": map[string]any{"PMQA": "auto", "Security": "auto"}},
		"launcher":      "auto",
	})
	overlayPath := filepath.Join(mustGetwd(t), OverlayFilename)
	writeJSONFile(t, overlayPath, map[string]any{"agent_language": "ja", "issue": map[string]any{"result_on_done": "off"}})
	storePolicy(t, home, policySnapshot(t, "e1", 3, validPolicyDocument()))
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := ValidatePolicyValues(policyJSON(t, validPolicyDocument()))
	if !reflect.DeepEqual(PolicyValuesOf(cfg), want) {
		t.Fatalf("values %+v, want %+v", PolicyValuesOf(cfg), want)
	}
	if cfg.Launcher != "auto" || cfg.Policy.Status() != (PolicyStatus{Epoch: "e1", Revision: 3, Source: PolicySourceServerCache}) {
		t.Fatalf("launcher %q status %+v", cfg.Launcher, cfg.Policy.Status())
	}
	overrides := map[string]PolicyOverride{}
	for _, o := range cfg.Policy.Overrides {
		overrides[o.Path] = o
	}
	checks := []PolicyOverride{
		{Path: "git.branch_mode", Source: scopePath, Local: "main-develop", Policy: "main-only"},
		{Path: "review_stages.large.PMQA", Source: scopePath, Local: "auto", Policy: "required"},
		{Path: "review_stages.large.Security", Source: scopePath, Local: "auto", Policy: "skip"},
		{Path: "agent_language", Source: overlayPath, Local: "ja", Policy: "zh-CN"},
		{Path: "issue.result_on_done", Source: overlayPath, Local: "off", Policy: "ask"},
	}
	for _, check := range checks {
		if got := overrides[check.Path]; got != check {
			t.Errorf("override %s = %+v, want %+v", check.Path, got, check)
		}
	}
	if len(overrides) != len(checks) {
		t.Errorf("overrides %+v", cfg.Policy.Overrides)
	}
	if !hasPolicyNotice(cfg, NoticePolicyOverride) {
		t.Fatal("no override notice")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func hasPolicyNotice(cfg *Config, code string) bool {
	return slices.ContainsFunc(cfg.Policy.Notices, func(n PolicyNotice) bool { return n.Code == code })
}

func TestLoadKeepsLocalValuesWithoutAUsablePolicy(t *testing.T) {
	cases := []struct {
		name   string
		cache  func(t *testing.T, home string)
		notice string
	}{
		{"missing", func(*testing.T, string) {}, NoticePolicyCacheMissing},
		{"unset", func(t *testing.T, home string) { storePolicy(t, home, PolicySnapshot{}) }, NoticePolicyUnset},
		{"damaged", func(t *testing.T, home string) {
			path := PolicyCachePath(home, "http://example.test:7878", ProjectIDForName("golf"))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, NoticePolicyCacheInvalid},
		{"invalid values", func(t *testing.T, home string) {
			doc := validPolicyDocument()
			delete(doc, "rules")
			storePolicy(t, home, policySnapshot(t, "e1", 1, doc))
		}, NoticePolicyCacheInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, _ := isolatePolicyClient(t, map[string]any{"git": map[string]any{"branch_mode": "main-develop"}})
			c.cache(t, home)
			cfg, err := Load(false)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Git.BranchMode != "main-develop" || cfg.Policy.Status().Source != PolicySourceLocal || !hasPolicyNotice(cfg, c.notice) {
				t.Fatalf("branch mode %q policy %+v", cfg.Git.BranchMode, cfg.Policy)
			}
		})
	}
}

func TestLoadWithoutAProjectHasNoPolicyRemark(t *testing.T) {
	isolatePolicyClient(t, map[string]any{"project": map[string]any{}})
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Policy.Notices) != 0 || cfg.Policy.Status() != (PolicyStatus{Source: PolicySourceLocal}) {
		t.Fatalf("policy %+v", cfg.Policy)
	}
}

func TestLoadIgnoresUnknownCachedKeysAndFillsMissingModules(t *testing.T) {
	home, _ := isolatePolicyClient(t, nil)
	doc := validPolicyDocument()
	doc["rules"] = map[string]any{"review": false, "future_module": true}
	storePolicy(t, home, policySnapshot(t, "e1", 2, doc))
	cfg, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rules[RuleReview] || !cfg.Rules[RuleGit] || cfg.Policy.Source != PolicySourceServerCache || !hasPolicyNotice(cfg, NoticePolicyIgnored) {
		t.Fatalf("rules %v policy %+v", cfg.Rules, cfg.Policy)
	}
	if _, ok := cfg.Rules["future_module"]; ok {
		t.Fatal("the unknown module reached the rules")
	}
}

func TestStorePolicyWritesOnlyANewerPolicyVersion(t *testing.T) {
	home, _ := isolatePolicyClient(t, nil)
	read := func() PolicyCache {
		return ReadPolicyCache(home, "http://EXAMPLE.test:7878/", ProjectIDForName("golf"))
	}
	if !storePolicy(t, home, policySnapshot(t, "e1", 2, validPolicyDocument())) {
		t.Fatal("the first policy was not stored")
	}
	if storePolicy(t, home, policySnapshot(t, "e1", 1, validPolicyDocument())) || storePolicy(t, home, policySnapshot(t, "e1", 2, validPolicyDocument())) {
		t.Fatal("an older or equal policy version replaced the cache")
	}
	if got := read(); got.Status != PolicyCacheSet || got.Snapshot.Revision != 2 {
		t.Fatalf("cache %+v", got)
	}
	if !storePolicy(t, home, policySnapshot(t, "e1", 3, validPolicyDocument())) || !storePolicy(t, home, policySnapshot(t, "e2", 1, validPolicyDocument())) {
		t.Fatal("a newer revision or another epoch was not stored")
	}
	if !storePolicy(t, home, PolicySnapshot{}) {
		t.Fatal("an unset answer did not replace the cached policy")
	}
	if got := read(); got.Status != PolicyCacheUnset || len(got.Snapshot.Values) != 0 {
		t.Fatalf("cache %+v", got)
	}
	if storePolicy(t, home, PolicySnapshot{}) {
		t.Fatal("a second unset answer was written again")
	}
}

func TestPolicyCacheIsPrivateAtomicAndRefusesLinks(t *testing.T) {
	home, _ := isolatePolicyClient(t, nil)
	storePolicy(t, home, policySnapshot(t, "e1", 1, validPolicyDocument()))
	path := PolicyCachePath(home, "http://example.test:7878", ProjectIDForName("golf"))
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if name := entry.Name(); name != filepath.Base(path) && name != filepath.Base(path)+".lock" {
			t.Errorf("a temporary file stayed behind: %s", name)
		}
	}
	if runtime.GOOS != "windows" {
		for _, check := range []struct {
			path string
			mode os.FileMode
		}{{path, 0o600}, {filepath.Dir(path), 0o700}, {home, 0o700}} {
			info, err := os.Stat(check.path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != check.mode {
				t.Errorf("%s has mode %v, want %v", check.path, info.Mode().Perm(), check.mode)
			}
		}
	}
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if got := ReadPolicyCache(home, "http://example.test:7878", ProjectIDForName("golf")); got.Status != PolicyCacheInvalid {
		t.Fatalf("a symlinked cache read as %v", got.Status)
	}
	if _, err := StorePolicy(home, "http://example.test:7878", ProjectIDForName("golf"), policySnapshot(t, "e1", 2, validPolicyDocument())); err == nil {
		if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("the store wrote through the symlink")
		}
	}
	if after, _ := os.ReadFile(target); string(after) != string(data) {
		t.Fatal("the symlink target changed")
	}
}

func TestPolicyCacheKeysByCanonicalOriginAndProjectID(t *testing.T) {
	if PolicyCachePath("/h", "HTTP://Example.TEST:7878/", "p-1") != PolicyCachePath("/h", "http://example.test:7878", "p-1") {
		t.Fatal("two spellings of one server do not share a cache")
	}
	if PolicyCachePath("/h", "http://example.test:7878", "p-1") == PolicyCachePath("/h", "http://example.test:7879", "p-1") ||
		PolicyCachePath("/h", "http://example.test:7878", "p-1") == PolicyCachePath("/h", "http://example.test:7878", "p-2") {
		t.Fatal("different servers or projects share a cache")
	}
	home, _ := isolatePolicyClient(t, nil)
	storePolicy(t, home, policySnapshot(t, "e1", 1, validPolicyDocument()))
	// A cache file copied under the name of another project is never applied.
	source := PolicyCachePath(home, "http://example.test:7878", ProjectIDForName("golf"))
	other := PolicyCachePath(home, "http://example.test:7878", ProjectIDForName("chess"))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadPolicyCache(home, "http://example.test:7878", ProjectIDForName("chess")); got.Status != PolicyCacheInvalid {
		t.Fatalf("a foreign cache read as %v", got.Status)
	}
}

func TestConfigWritesNeverStoreCachedPolicyValues(t *testing.T) {
	home, scopePath := isolatePolicyClient(t, map[string]any{"git": map[string]any{"branch_mode": "main-develop"}})
	overlayPath := filepath.Join(mustGetwd(t), OverlayFilename)
	writeJSONFile(t, overlayPath, map[string]any{"agent_language": "ja"})
	storePolicy(t, home, policySnapshot(t, "e1", 1, validPolicyDocument()))
	loaded, err := Load(false)
	if err != nil || loaded.Git.BranchMode != "main-only" {
		t.Fatalf("policy not applied: %v %+v", err, loaded)
	}
	// Update, Save of a scope read, the sparse overlay write and the doctor repair start from
	// the files themselves.
	if _, err := Update(func(cfg *Config) error { cfg.Launcher = "auto"; return nil }); err != nil {
		t.Fatal(err)
	}
	scope, err := LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(scope); err != nil {
		t.Fatal(err)
	}
	overlay, err := ReadOverlayFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := CloneOverlay(overlay)
	OverlaySet(edited, "tmux", "launcher")
	if _, err := SaveOverlayIfUnchanged(overlayPath, edited, overlay); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Repair(nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{scopePath, overlayPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"main-only", "zh-CN", `"required"`, `"ask"`} {
			if strings.Contains(string(data), value) {
				t.Errorf("%s holds the policy value %s:\n%s", path, value, data)
			}
		}
	}
}
