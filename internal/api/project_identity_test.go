package api

import (
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

var (
	acceptedProjectNames = []string{"golf", "Golf", "a", "golf_v2-x", "g" + strings.Repeat("x", 127)}
	refusedProjectNames  = []string{"", "1golf", "_golf", "-golf", "go.lf", "go lf", "gölf", "g" + strings.Repeat("x", 128)}
)

func TestProjectNameRule(t *testing.T) {
	for _, name := range acceptedProjectNames {
		if !ValidProjectName(name) {
			t.Errorf("refused %q", name)
		}
	}
	for _, name := range refusedProjectNames {
		if ValidProjectName(name) {
			t.Errorf("accepted %q", name)
		}
	}
}

// internal/config keeps its own copy of the rule because it cannot import
// this package; both copies must judge every name the same way.
func TestProjectNameRuleMatchesConfig(t *testing.T) {
	for _, name := range append(slices.Concat(acceptedProjectNames, refusedProjectNames), "Z", "z9", "9z", "a.b", "a:b") {
		if ValidProjectName(name) != config.ValidProjectName(name) {
			t.Errorf("api and config disagree on %q", name)
		}
	}
}

func TestProjectIdentityDerivesFromTheExactName(t *testing.T) {
	cases := []struct{ name, id, dir string }{
		{"golf", "p-625fe74cad4600b5", "golf-625fe74cad4600b5"},
		{"Golf", "p-007aafd4afea7396", "golf-007aafd4afea7396"},
	}
	for _, c := range cases {
		if got := ProjectID(c.name); got != c.id {
			t.Errorf("ProjectID(%q) = %q, want %q", c.name, got, c.id)
		}
		if got := ProjectDirName(c.name); got != c.dir {
			t.Errorf("ProjectDirName(%q) = %q, want %q", c.name, got, c.dir)
		}
	}
}

func TestProjectDirNameCutsTheReadablePrefix(t *testing.T) {
	name := "A" + strings.Repeat("b", 127)
	dir := ProjectDirName(name)
	prefix, hash, ok := strings.Cut(dir, "-")
	if !ok || prefix != "a"+strings.Repeat("b", 47) || hash != strings.TrimPrefix(ProjectID(name), "p-") {
		t.Fatalf("ProjectDirName(%q) = %q", name, dir)
	}
}
