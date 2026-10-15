package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestPiDirectoryTargets(t *testing.T) {
	home := setupInstallHome(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	outside := t.TempDir()
	for _, tt := range []struct {
		name, value, want string
	}{
		{"empty", "", filepath.Join(home, ".pi", "agent")},
		{"inside-home", filepath.Join(home, ".config", "pi"), filepath.Join(home, ".config", "pi")},
		{"outside-home", outside, outside},
		{"tilde", "~", home},
		{"tilde-prefix", "~/.config/pi", filepath.Join(home, ".config", "pi")},
		{"relative", "custom/pi", filepath.Join(cwd, "custom", "pi")},
		{"spaces", " pi config ", filepath.Join(cwd, " pi config ")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PI_CODING_AGENT_DIR", tt.value)
			paths := globalExtensionPaths(t, home)
			if got := mustRulesTarget(t, "pi", paths); got != filepath.Join(tt.want, "AGENTS.md") {
				t.Fatalf("rules=%q, root=%q", got, tt.want)
			}
			if got := mustExtensionTarget(t, "pi", paths); got != filepath.Join(tt.want, "extensions", "kander-rules.ts") {
				t.Fatalf("extension=%q, root=%q", got, tt.want)
			}
			for _, agent := range config.ExecutionAgents {
				if agent == "pi" {
					continue
				}
				spec, ok := config.RulesSpec(agent)
				if ok && spec.Global != "" {
					if got := mustRulesTarget(t, agent, paths); got != filepath.Join(home, filepath.FromSlash(spec.Global)) {
						t.Fatalf("%s affected by Pi override: %s", agent, got)
					}
				}
			}
			project := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: cwd}
			if got := mustRulesTarget(t, "pi", project); got != filepath.Join(cwd, "AGENTS.md") {
				t.Fatalf("project rules=%q", got)
			}
			if got := mustExtensionTarget(t, "pi", project); got != filepath.Join(cwd, ".pi", "extensions", "kander-rules.ts") {
				t.Fatalf("project extension=%q", got)
			}
		})
	}
	t.Run("unset", func(t *testing.T) {
		if err := os.Unsetenv("PI_CODING_AGENT_DIR"); err != nil {
			t.Fatal(err)
		}
		paths := globalExtensionPaths(t, home)
		if got := mustRulesTarget(t, "pi", paths); got != filepath.Join(home, ".pi", "agent", "AGENTS.md") {
			t.Fatalf("unset rules=%q", got)
		}
		if got := mustExtensionTarget(t, "pi", paths); got != piExtensionTarget(t, home, false) {
			t.Fatalf("unset extension=%q", got)
		}
	})
}

func TestPiDirectoryCoverageAndInstall(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	directory := filepath.Join(t.TempDir(), "pi")
	t.Setenv("PI_CODING_AGENT_DIR", directory)
	old := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(IntegrationAgents(paths), "pi") || slices.Contains(ExtensionAgents(paths), "pi") {
		t.Fatal("default directory must not cover a missing override")
	}
	integrateAgentRules(paths)
	integrateAgentExtensions(paths)
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatalf("missing override created: %v", err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(IntegrationAgents(paths), "pi") || !slices.Contains(ExtensionAgents(paths), "pi") {
		t.Fatal("existing override must cover Pi rules and extension")
	}
	for _, result := range integrateAgentRules(paths) {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	results := integrateAgentExtensions(paths)
	if len(results) != 1 || results[0].Err != nil || results[0].Status != ExtensionWritten {
		t.Fatalf("extension install: %+v", results)
	}
	if ok, detail := RulesIntegration("pi", paths); !ok {
		t.Fatalf("rules not integrated: %s", detail)
	}
	out, err := InspectExtension("pi", paths)
	if err != nil || out.Status != ExtensionInstalled {
		t.Fatalf("inspection: %+v %v", out, err)
	}
	for _, target := range []string{filepath.Join(old, "AGENTS.md"), piExtensionTarget(t, home, false)} {
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatalf("default target touched: %s: %v", target, err)
		}
	}
	modified := []byte("// user extension\n")
	if err := os.WriteFile(out.Target, modified, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = EnsureExtension("pi", paths)
	if err != nil || out.Status != ExtensionModified {
		t.Fatalf("modified: %+v %v", out, err)
	}
	if got, err := os.ReadFile(out.Target); err != nil || string(got) != string(modified) {
		t.Fatalf("modified extension overwritten: %q %v", got, err)
	}
}

func TestPiDirectoryIgnoresLegacySymlink(t *testing.T) {
	home := setupInstallHome(t)
	directory := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", directory)
	if err := os.Mkdir(filepath.Join(home, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(home, ".pi", "agent")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	paths := globalExtensionPaths(t, home)
	if _, err := EnsureRulesIntegration("pi", paths); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureExtension("pi", paths); err != nil {
		t.Fatal(err)
	}
	if out, err := InspectExtension("pi", paths); err != nil || out.Status != ExtensionInstalled {
		t.Fatalf("legacy link affected override: %+v %v", out, err)
	}
}

func TestPiDirectoryRejectsExtensionSymlinks(t *testing.T) {
	for _, component := range []string{"ancestor", "root", "extensions", "file"} {
		t.Run(component, func(t *testing.T) {
			home := setupInstallHome(t)
			root := t.TempDir()
			directory := filepath.Join(root, "pi")
			t.Setenv("PI_CODING_AGENT_DIR", directory)
			target := filepath.Join(directory, "extensions", "kander-rules.ts")
			victim := t.TempDir()
			link, dest := directory, victim
			switch component {
			case "ancestor":
				link = filepath.Join(root, "link")
				directory = filepath.Join(link, "pi")
				t.Setenv("PI_CODING_AGENT_DIR", directory)
			case "extensions":
				link = filepath.Dir(target)
			case "file":
				link = target
				dest = filepath.Join(victim, "untouched.ts")
				if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(dest, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			paths := globalExtensionPaths(t, home)
			if _, err := InspectExtension("pi", paths); err == nil {
				t.Fatal("inspection followed symlink")
			}
			if _, err := EnsureExtension("pi", paths); err == nil {
				t.Fatal("write followed symlink")
			}
			entries, err := os.ReadDir(victim)
			if err != nil {
				t.Fatal(err)
			}
			if component == "file" {
				if data, err := os.ReadFile(dest); err != nil || string(data) != "original" {
					t.Fatalf("symlink target changed: %q %v", data, err)
				}
			} else if len(entries) != 0 {
				t.Fatalf("redirected directory changed: %v", entries)
			}
		})
	}
}

func mustRulesTarget(t *testing.T, agent string, paths config.InstallPaths) string {
	t.Helper()
	target, err := AgentRulesTarget(agent, paths)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func mustExtensionTarget(t *testing.T, agent string, paths config.InstallPaths) string {
	t.Helper()
	target, err := extensionTarget(agent, paths)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestAgentConfigEnvTargets(t *testing.T) {
	home := setupInstallHome(t)
	custom := t.TempDir()
	xdg := t.TempDir()
	for _, tt := range []struct {
		agent string
		env   map[string]string
		want  string
	}{
		{"codex", map[string]string{"CODEX_HOME": custom}, filepath.Join(custom, "AGENTS.md")},
		{"claude", map[string]string{"CLAUDE_CONFIG_DIR": custom}, filepath.Join(custom, "CLAUDE.md")},
		{"grok", map[string]string{"GROK_HOME": custom}, filepath.Join(custom, "AGENTS.md")},
		{"kimi", map[string]string{"KIMI_CODE_HOME": custom}, filepath.Join(custom, "AGENTS.md")},
		{"opencode", map[string]string{"XDG_CONFIG_HOME": xdg}, filepath.Join(xdg, "opencode", "AGENTS.md")},
		{"opencode", map[string]string{"OPENCODE_CONFIG_DIR": custom, "XDG_CONFIG_HOME": xdg}, filepath.Join(custom, "AGENTS.md")},
		{"opencode", map[string]string{"XDG_CONFIG_HOME": "relative"}, filepath.Join(home, ".config", "opencode", "AGENTS.md")},
		{"devin", map[string]string{"XDG_CONFIG_HOME": xdg}, filepath.Join(xdg, "devin", "AGENTS.md")},
		{"devin", map[string]string{"XDG_CONFIG_HOME": "~/xdg"}, filepath.Join(home, ".config", "devin", "AGENTS.md")},
		{"cursor", map[string]string{"CODEX_HOME": custom, "XDG_CONFIG_HOME": xdg}, filepath.Join(home, ".cursor", "AGENTS.md")},
	} {
		t.Run(tt.agent, func(t *testing.T) {
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			paths := globalExtensionPaths(t, home)
			if got := mustRulesTarget(t, tt.agent, paths); got != tt.want {
				t.Fatalf("target=%q want %q", got, tt.want)
			}
			project := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: custom}
			spec, _ := config.RulesSpec(tt.agent)
			if got := mustRulesTarget(t, tt.agent, project); got != filepath.Join(custom, filepath.FromSlash(spec.Project)) {
				t.Fatalf("project target=%q", got)
			}
		})
	}
	t.Run("defaults", func(t *testing.T) {
		paths := globalExtensionPaths(t, home)
		for _, agent := range config.ExecutionAgents {
			spec, _ := config.RulesSpec(agent)
			if got := mustRulesTarget(t, agent, paths); got != filepath.Join(home, filepath.FromSlash(spec.Global)) {
				t.Fatalf("%s default target=%q", agent, got)
			}
		}
	})
}

// scopeConfigPaths writes a scope config.json with agents.<agent>.config_dir and
// returns global paths that read it.
func scopeConfigPaths(t *testing.T, home, agent, dir string) config.InstallPaths {
	t.Helper()
	paths := globalExtensionPaths(t, home)
	paths.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	writeScopeConfig(t, paths.ConfigPath, agent, dir)
	return paths
}

func writeScopeConfig(t *testing.T, file, agent, dir string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Agents = map[string]config.AgentDefinition{agent: {ConfigDir: dir}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScopeConfigDirOverridesEnvironment(t *testing.T) {
	home := setupInstallHome(t)
	configured := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	paths := scopeConfigPaths(t, home, "codex", configured)
	if got := mustRulesTarget(t, "codex", paths); got != filepath.Join(configured, "AGENTS.md") {
		t.Fatalf("configured target=%q", got)
	}
	tilde := scopeConfigPaths(t, home, "codex", "~/codex-config")
	if got := mustRulesTarget(t, "codex", tilde); got != filepath.Join(home, "codex-config", "AGENTS.md") {
		t.Fatalf("tilde target=%q", got)
	}
	pi := scopeConfigPaths(t, home, "pi", configured)
	if got := mustExtensionTarget(t, "pi", pi); got != filepath.Join(configured, "extensions", "kander-rules.ts") {
		t.Fatalf("pi extension=%q", got)
	}
	project := paths
	project.Mode, project.ProjectRoot = config.ModeProject, t.TempDir()
	if got := mustRulesTarget(t, "codex", project); got != filepath.Join(project.ProjectRoot, "AGENTS.md") {
		t.Fatalf("project target=%q", got)
	}
}

func TestProjectOverlayConfigDirIgnored(t *testing.T) {
	home := setupInstallHome(t)
	work := t.TempDir()
	t.Chdir(work)
	overlay := `{"agents":{"codex":{"config_dir":` + strconv.Quote(t.TempDir()) + `}}}`
	if err := os.WriteFile(filepath.Join(work, config.OverlayFilename), []byte(overlay), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := globalExtensionPaths(t, home)
	paths.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if got := mustRulesTarget(t, "codex", paths); got != filepath.Join(home, ".codex", "AGENTS.md") {
		t.Fatalf("overlay changed target: %q", got)
	}
}

func TestInvalidScopeConfigReportsError(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	paths.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	writeScopeConfig(t, paths.ConfigPath, "codex", "relative")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AgentRulesTarget("codex", paths); err == nil {
		t.Fatal("invalid config_dir accepted")
	}
	if !slices.Contains(IntegrationAgents(paths), "codex") {
		t.Fatal("unresolvable agent dropped from coverage")
	}
	for _, result := range integrateAgentRules(paths) {
		if result.Agent == "codex" && result.Err == nil {
			t.Fatal("resolution error not reported")
		}
	}
}

func TestRelocatedConfigDirCoverageAndCleanup(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	directory := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CODEX_HOME", directory)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(IntegrationAgents(paths), "codex") {
		t.Fatal("default directory must not cover a missing relocated directory")
	}
	integrateAgentRules(paths)
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatalf("missing relocated directory created: %v", err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "AGENTS.md")
	entry := RulesEntry(paths)
	if err := os.WriteFile(target, []byte(entry+"\n"+entry+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	issues, err := InspectRulesReferences("codex", paths)
	if err != nil || issues.Duplicates != 1 || len(issues.Files) != 1 || issues.Files[0].Target != target {
		t.Fatalf("inspection: %+v %v", issues, err)
	}
	if out, err := EnsureRulesIntegration("codex", paths); err != nil || out.Target != target {
		t.Fatalf("repair: %+v %v", out, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("default target touched: %v", err)
	}
}
