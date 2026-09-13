package install

import (
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// assertHandoffInvocation checks that the installed binary starts without a
// --lang override and with exactly one KANDER_LANG default for the wizard language.
func assertHandoffInvocation(t *testing.T, dest, lang string, argv, env []string) {
	t.Helper()
	if !slices.Equal(argv, []string{dest}) {
		t.Fatalf("handoff argv=%q want only %q", argv, dest)
	}
	var langEntries []string
	for _, entry := range env {
		if strings.HasPrefix(entry, config.EnvLangCLI+"=") {
			t.Fatalf("handoff env keeps %s: %v", config.EnvLangCLI, env)
		}
		if strings.HasPrefix(entry, config.EnvLang+"=") {
			langEntries = append(langEntries, entry)
		}
	}
	if want := []string{config.EnvLang + "=" + lang}; !slices.Equal(langEntries, want) {
		t.Fatalf("handoff %s entries=%q want %q", config.EnvLang, langEntries, want)
	}
	if !slices.Contains(env, EnvPostInstall+"=1") {
		t.Fatalf("missing %s in env: %v", EnvPostInstall, env)
	}
}

// TestHandoffLanguageLetsConfigLanguageWin replays the handed-off process from
// the argv and environment launchInstalled passes, so a reintroduced --lang
// would pin the install language above the config language again.
func TestHandoffLanguageLetsConfigLanguageWin(t *testing.T) {
	setupInstallHome(t)
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(name, "")
	}
	var gotArgv, gotEnv []string
	handoff = func(_ string, argv, env []string) error {
		gotArgv, gotEnv = argv, env
		return nil
	}
	t.Cleanup(func() { handoff = defaultHandoff })
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvLang, "en")
	if err := launchInstalled("kander", "ja"); err != nil {
		t.Fatal(err)
	}

	for _, entry := range gotEnv {
		name, value, _ := strings.Cut(entry, "=")
		t.Setenv(name, value)
	}
	// Startup applies argv the same way cli.Run does.
	config.ApplyLanguageArgument(gotArgv)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })

	if got := config.ResolveLanguage(); got != "ja" {
		t.Fatalf("without a config language ResolveLanguage=%q want install language ja", got)
	}
	config.BindConfigLanguage(&config.Config{Language: "en"})
	if got := config.ResolveLanguage(); got != "en" {
		t.Fatalf("with config language en ResolveLanguage=%q want en", got)
	}
}
