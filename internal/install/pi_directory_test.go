package install

import (
	"os"
	"path/filepath"
	"slices"
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
			if got := AgentRulesTarget("pi", paths); got != filepath.Join(tt.want, "AGENTS.md") {
				t.Fatalf("rules=%q, root=%q", got, tt.want)
			}
			if got := extensionTarget("pi", paths); got != filepath.Join(tt.want, "extensions", "kander-rules.ts") {
				t.Fatalf("extension=%q, root=%q", got, tt.want)
			}
			for _, agent := range config.ExecutionAgents {
				if agent == "pi" {
					continue
				}
				spec, ok := config.RulesSpec(agent)
				if ok && spec.Global != "" {
					if got := AgentRulesTarget(agent, paths); got != filepath.Join(home, filepath.FromSlash(spec.Global)) {
						t.Fatalf("%s affected by Pi override: %s", agent, got)
					}
				}
			}
			project := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: cwd}
			if got := AgentRulesTarget("pi", project); got != filepath.Join(cwd, "AGENTS.md") {
				t.Fatalf("project rules=%q", got)
			}
			if got := extensionTarget("pi", project); got != filepath.Join(cwd, ".pi", "extensions", "kander-rules.ts") {
				t.Fatalf("project extension=%q", got)
			}
		})
	}
	t.Run("unset", func(t *testing.T) {
		if err := os.Unsetenv("PI_CODING_AGENT_DIR"); err != nil {
			t.Fatal(err)
		}
		paths := globalExtensionPaths(t, home)
		if got := AgentRulesTarget("pi", paths); got != filepath.Join(home, ".pi", "agent", "AGENTS.md") {
			t.Fatalf("unset rules=%q", got)
		}
		if got := extensionTarget("pi", paths); got != piExtensionTarget(t, home, false) {
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
