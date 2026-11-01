package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install/installtest"
)

func hasWarning(lines []ReportLine) bool {
	for _, line := range lines {
		if line.Level == LevelWarning {
			return true
		}
	}
	return false
}

func setupSwitchHome(t *testing.T) (string, config.InstallPaths) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
	writeRulesFile(t, filepath.Join(paths.RulesDir, "KANDER-AGENTS.md"), "# Kander entry\n")
	for _, dir := range []string{".codex", ".pi/agent"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, paths
}

func TestDoctorReportsSwitchOffWithoutWriting(t *testing.T) {
	home, paths := setupSwitchHome(t)
	existing := map[string]string{
		filepath.Join(home, ".codex", "AGENTS.md"):                           "# Mine\n",
		filepath.Join(home, ".codex", "AGENTS.override.md"):                  "# Override\n",
		filepath.Join(home, ".pi", "agent", "extensions", "kander-rules.ts"): "// old\n",
	}
	for path, content := range existing {
		writeRulesFile(t, path, content)
	}
	off := false
	cfg := config.DefaultConfig()
	config.SetAgentInstallRules(cfg, "codex", &off)
	config.SetAgentInstallRules(cfg, "pi", &off)
	switches := config.InstallRulesOf(cfg)
	for _, repair := range []bool{true, false} {
		var healthy bool
		lines := CaptureReport(func() {
			healthy = reportRulesIntegration(paths, switches, repair) && reportAgentExtensions(paths, switches, repair)
		})
		text := reportText(lines)
		if !healthy {
			t.Fatalf("repair=%v: turned-off agents made doctor unhealthy:\n%s", repair, text)
		}
		disabled := config.Text("menu.rules_injection_disabled", "Codex", filepath.Join(home, ".codex", "AGENTS.md"))
		if !strings.Contains(text, disabled) {
			t.Fatalf("repair=%v: missing turned-off line:\n%s", repair, text)
		}
		if hasWarning(lines) {
			t.Fatalf("repair=%v: turned-off agent reported as a problem:\n%s", repair, text)
		}
	}
	for path, content := range existing {
		if got, err := os.ReadFile(path); err != nil || string(got) != content {
			t.Fatalf("%s changed: %q %v", path, got, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".pi", "agent", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("pi rules file written: %v", err)
	}
}

func TestDoctorReportsCommentedReferenceInactive(t *testing.T) {
	home, paths := setupSwitchHome(t)
	target := filepath.Join(home, ".codex", "AGENTS.md")
	writeRulesFile(t, target, "# Mine\n\n<!-- read `"+filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")+"` -->\n")
	var healthy bool
	lines := CaptureReport(func() { healthy = reportRulesIntegration(paths, config.InstallRulesOf(nil), true) })
	text := reportText(lines)
	if healthy {
		t.Fatalf("inactive reference reported healthy:\n%s", text)
	}
	if !strings.Contains(text, config.Text("menu.rules_reference_inactive", "Codex", target)) {
		t.Fatalf("missing inactive line:\n%s", text)
	}
	if strings.Contains(text, config.Text("menu.is_connected_to_kander_rules", "Codex", target)) {
		t.Fatalf("inactive reference reported connected:\n%s", text)
	}
}

func TestOptionsFinishUsesSwitchesBeingSaved(t *testing.T) {
	home, _ := setupSwitchHome(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"pi": {ConfigDir: filepath.Join(home, ".pi", "agent")}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetAgentInstallRules("codex", false); err != nil {
		t.Fatal(err)
	}
	if err := session.SetAgentInstallRules("pi", false); err != nil {
		t.Fatal(err)
	}
	lines, err := session.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("options save wrote codex before saving its switch:\n%s", reportText(lines))
	}
	if _, err := os.Lstat(filepath.Join(home, ".pi", "agent", "extensions", "kander-rules.ts")); !os.IsNotExist(err) {
		t.Fatalf("options save wrote the pi extension: %v", err)
	}
	if _, err := session.SaveAllDirty(); err != nil {
		t.Fatal(err)
	}
	saved, err := config.LoadScope(false)
	if err != nil {
		t.Fatal(err)
	}
	switches := config.InstallRulesOf(saved)
	if switches.Enabled("codex") || switches.Enabled("pi") || !switches.Enabled("claude") {
		t.Fatalf("saved switches=%+v", switches)
	}
	if saved.Agents["pi"].ConfigDir != filepath.Join(home, ".pi", "agent") {
		t.Fatalf("save dropped config_dir: %+v", saved.Agents["pi"])
	}
}

func TestSessionAgentSwitchFollowsGlobalWhenEqual(t *testing.T) {
	session, err := NewSessionForTest(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetAgentInstallRules("codex", false); err != nil {
		t.Fatal(err)
	}
	if err := session.SetAgentInstallRules("codex", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := session.Config.Agents["codex"]; ok {
		t.Fatal("an agent value equal to the global switch stays")
	}
	if err := session.SetInstallRules(false); err != nil {
		t.Fatal(err)
	}
	if session.InstallRules().Enabled("codex") {
		t.Fatal("codex does not follow the global switch")
	}
	if err := session.SetAgentInstallRules("wrapper", false); err == nil {
		t.Fatal("custom agent switch accepted")
	}
}

func TestDoctorWarnsAboutOverlayInstallRules(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	overlay := filepath.Join(work, config.OverlayFilename)
	if err := os.WriteFile(overlay, []byte(`{"install_rules":false,"agents":{"codex":{"install_rules":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	text := reportText(CaptureReport(reportOverlayInstallRules))
	for _, key := range []string{"install_rules", "agents.codex.install_rules"} {
		if !strings.Contains(text, config.Text("menu.overlay_install_rules_ignored", key, overlay)) {
			t.Fatalf("missing warning for %s:\n%s", key, text)
		}
	}
	if strings.Count(text, "\n")+1 != 2 {
		t.Fatalf("one warning per key expected:\n%s", text)
	}
}
