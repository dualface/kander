package review

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// abandon is an evidence command of server mode only, and its schema needs no
// server binding.
func TestAbandonSchemaNeedsNoServerAndSpeaksEveryLanguage(t *testing.T) {
	if !remoteDispositionCommands["abandon"] || !schemaCommand("abandon") {
		t.Fatal("abandon is not an evidence command with a schema")
	}
	registered := false
	for _, sub := range NewCommand().Commands() {
		registered = registered || sub.Name() == "abandon"
	}
	if !registered {
		t.Fatal("kander review has no abandon subcommand")
	}
	dir := t.TempDir()
	setupLang(t, filepath.Join(dir, "config.json"))
	for _, lang := range []string{"en", "cn", "ja"} {
		t.Setenv(config.EnvLang, lang)
		config.BindEffectiveLanguage()
		out := schemaOutput(t, "abandon")
		if !strings.HasPrefix(out, "command: abandon\n") {
			t.Fatalf("%s: %q", lang, out)
		}
		for _, field := range []string{"run_id", "decision", "reason"} {
			description := config.Text(schemaTextID("abandon", field))
			if !strings.Contains(out, "field: "+field+"\nrequired: yes\n") || !strings.Contains(out, description) || strings.HasPrefix(description, "review.schema.") {
				t.Fatalf("%s: %s undocumented:\n%s", lang, field, out)
			}
		}
	}
}
