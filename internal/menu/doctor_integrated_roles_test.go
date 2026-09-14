package menu

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestDoctorIntegratedRoleAvailability(t *testing.T) {
	for _, scale := range config.TaskScales {
		for _, role := range config.ReviewRoles {
			for _, stage := range []string{"skip", "auto", "required"} {
				t.Run(scale+"/"+role+"/"+stage, func(t *testing.T) {
					cfg, paths, agents := doctorFourRoleConfig(t)
					cfg.ReviewStages[scale][role] = stage
					cfg.Reviewers[scale][role] = "codex"
					cfg.Models.ReviewRoles[role][scale+"_model"] = "preserved-model"
					cfg.Models.ReviewRoles[role][scale+"_effort"] = "high"
					cfg.Models.ReviewRoles[role][scale+"_agent"] = "codex"
					before := config.Clone(cfg)
					if healthy := validateConfiguredResources(cfg, agents, paths, TerminalTools{}); healthy {
						t.Errorf("healthy=%v want false", healthy)
					}
					changes := repairConfiguredTools(cfg, cfg, agents, TerminalTools{})
					if cfg.Reviewers[scale][role] != "claude" || cfg.Models.ReviewRoles[role][scale+"_agent"] != "claude" || cfg.Models.ReviewRoles[role][scale+"_model"] != cfg.Models.Review["claude"]["model"] {
						t.Fatalf("unavailable reviewer was not repaired: %v", changes)
					}
					// Only the selected scale/role needs repair, including when the other scale skips it.
					before.Reviewers[scale][role] = cfg.Reviewers[scale][role]
					for _, field := range []string{"model", "effort", "agent"} {
						before.Models.ReviewRoles[role][scale+"_"+field] = cfg.Models.ReviewRoles[role][scale+"_"+field]
					}
					if len(changes) != 1 || !reflect.DeepEqual(cfg, before) {
						t.Fatalf("repair changed unrelated configuration: %v", changes)
					}
				})
			}
		}
	}
}

func doctorFourRoleConfig(t *testing.T) (*config.Config, config.InstallPaths, map[string]agentState) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	cfg, err := config.ValidateJSON([]byte(`{
		"schema_version": 1, "welcome_complete": true,
		"kanban_agent": "claude", "launcher": "foreground",
		"reviewers": {"PM":"claude","QA":"claude","CSA":"claude","Hacker":"claude"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	name := "kander"
	if isWindowsOS() {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte("test review gate"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := config.InstallPaths{Mode: config.ModeProject, BinDir: root}
	agents := map[string]agentState{"claude": {Path: "claude", Version: "test-version", Review: true, Execution: true}}
	return cfg, paths, agents
}
