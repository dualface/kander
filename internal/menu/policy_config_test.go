package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
)

const cachedPolicyValues = `{"git":{"branch_mode":"main-only"},"review_stages":{"large":{"PMQA":"required","Security":"skip"},"small":{"PMQA":"auto","Security":"auto"}},` +
	`"rules":{"code":true,"collaboration":true,"git":true,"reporting":true,"review":true,"task_groups":true,"task_intake":true},"agent_language":"zh-CN","issue":{"result_on_done":"ask"}}`

// boundPolicyConfig writes a scope configuration bound to a project, in a fresh client home and
// working directory, and returns the client home.
func boundPolicyConfig(t *testing.T, project string) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "client")
	t.Setenv(config.EnvClientHome, home)
	t.Setenv(config.EnvConfig, filepath.Join(root, "config.json"))
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.Server.URL = "http://127.0.0.1:7878"
	cfg.Project.Name = project
	cfg.Git.BranchMode = config.GitBranchModeMainDevelop
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return home
}

type policyConfigResult struct {
	Git           config.Git          `json:"git"`
	ReviewStages  map[string]any      `json:"review_stages"`
	ProjectPolicy config.PolicyStatus `json:"project_policy"`
	Launcher      string              `json:"launcher"`
	Notices       []command.Notice    `json:"notices"`
}

func runPolicyConfigJSON(t *testing.T) policyConfigResult {
	t.Helper()
	code, stdout, stderr := commandtest.Capture(func() int { return commandtest.Run(NewConfigCommand(), "--json") })
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var result policyConfigResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func noticeCodes(notices []command.Notice) string {
	var codes []string
	for _, n := range notices {
		codes = append(codes, n.Code)
	}
	return strings.Join(codes, ",")
}

func TestConfigJSONShowsTheCachedProjectPolicy(t *testing.T) {
	home := boundPolicyConfig(t, "golf")
	if _, err := config.StorePolicy(home, "http://127.0.0.1:7878", config.ProjectIDForName("golf"), config.PolicySnapshot{
		Epoch: "e1", Revision: 4, Values: json.RawMessage(cachedPolicyValues), UpdatedAt: time.Now(), UpdatedBy: "dev",
	}); err != nil {
		t.Fatal(err)
	}
	result := runPolicyConfigJSON(t)
	if result.ProjectPolicy != (config.PolicyStatus{Epoch: "e1", Revision: 4, Source: config.PolicySourceServerCache}) {
		t.Fatalf("project_policy %+v", result.ProjectPolicy)
	}
	if result.Git.BranchMode != "main-only" || result.ReviewStages["large"].(map[string]any)["PMQA"] != "required" || result.Launcher == "" {
		t.Fatalf("values %+v", result)
	}
	if !strings.Contains(noticeCodes(result.Notices), config.NoticePolicyOverride) {
		t.Fatalf("notices %+v", result.Notices)
	}
	code, stdout, _ := commandtest.Capture(func() int { return commandtest.Run(NewConfigCommand()) })
	if code != 0 || !strings.Contains(stdout, "server policy revision 4 from the cache") {
		t.Fatalf("human output: %q", stdout)
	}
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	lines := CaptureReport(func() { reportPolicyOverrides(cfg) })
	text := ""
	for _, line := range lines {
		text += line.Text + "\n"
	}
	if !strings.Contains(text, "git.branch_mode") || !strings.Contains(text, "main-develop") || !strings.Contains(text, "main-only") {
		t.Fatalf("doctor lines:\n%s", text)
	}
	for _, line := range lines {
		if line.Level != LevelInfo {
			t.Fatalf("an override line is not a hint: %+v", line)
		}
	}
}

func TestConfigJSONWithoutACacheUsesTheLocalValues(t *testing.T) {
	boundPolicyConfig(t, "golf")
	result := runPolicyConfigJSON(t)
	if result.ProjectPolicy != (config.PolicyStatus{Source: config.PolicySourceLocal}) || result.Git.BranchMode != "main-develop" ||
		noticeCodes(result.Notices) != config.NoticePolicyCacheMissing {
		t.Fatalf("result %+v", result)
	}
}

func TestConfigJSONUnboundHasNoPolicyNotice(t *testing.T) {
	boundPolicyConfig(t, "")
	result := runPolicyConfigJSON(t)
	if result.ProjectPolicy != (config.PolicyStatus{Source: config.PolicySourceLocal}) || len(result.Notices) != 0 {
		t.Fatalf("result %+v", result)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv(config.EnvClientHome), "policies")); !os.IsNotExist(err) {
		t.Fatalf("kander config created the cache directory: %v", err)
	}
}
