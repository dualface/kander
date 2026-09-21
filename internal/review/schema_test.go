package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestEvidenceSchemaPrintsEveryField(t *testing.T) {
	dir := t.TempDir()
	setupLang(t, filepath.Join(dir, "config.json"))
	t.Chdir(dir)
	for _, command := range schemaCommands {
		out := schemaOutput(t, command)
		if !strings.HasPrefix(out, "command: "+command+"\n") {
			t.Fatalf("%s prefix: %q", command, out)
		}
		if strings.Contains(out, "review.schema.") {
			t.Fatalf("%s leaked a message id", command)
		}
		if !strings.Contains(out, "\nfield: ") || !strings.Contains(out, "\nrequired: ") || !strings.Contains(out, "\ndescription: ") {
			t.Fatalf("%s missing field table", command)
		}
	}
	if !strings.Contains(schemaOutput(t, "assign"), "field: items\nrequired: yes\n") {
		t.Fatal("assign items")
	}
	if !strings.Contains(schemaOutput(t, "advance"), "field: advance.foreign_commits\nrequired: no\n") {
		t.Fatal("foreign commits")
	}
	if !strings.Contains(schemaOutput(t, "disposition"), "values: confirmed | fixed | rejected | unverifiable | waived | deferred\n") {
		t.Fatal("status values")
	}
	if !strings.Contains(schemaOutput(t, "close"), "field: roles.<key>.passed_at\nrequired: yes\n") {
		t.Fatal("passed_at")
	}
	if !strings.Contains(schemaOutput(t, "close"), ":(literal)PATH") {
		t.Fatal("diff hash command")
	}
	if _, err := os.Stat(filepath.Join(dir, "kanban")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	code, out, _ := captureRun(t, []string{"plan", "--schema", dir})
	if code == 0 || out != "" {
		t.Fatalf("extra argument: code %d stdout %q", code, out)
	}
	code, out, _ = captureRun(t, []string{"map-legacy", "--schema"})
	if code == 0 || strings.Contains(out, "command: map-legacy") {
		t.Fatalf("map-legacy schema: code %d stdout %q", code, out)
	}
}

func TestEvidenceSchemaUsesInterfaceLanguage(t *testing.T) {
	dir := t.TempDir()
	setupLang(t, filepath.Join(dir, "config.json"))
	t.Setenv(config.EnvLang, "cn")
	config.BindEffectiveLanguage()
	out := schemaOutput(t, "plan")
	if !strings.Contains(out, "field: schema\n") || !strings.Contains(out, "新计划必须为 1") {
		t.Fatalf("chinese description missing:\n%s", out)
	}
}

func schemaOutput(t *testing.T, command string) string {
	t.Helper()
	code, out, errText := captureRun(t, []string{command, "--schema"})
	if code != 0 {
		t.Fatalf("%s: %s", command, errText)
	}
	return out
}
