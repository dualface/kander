package rules_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
	"github.com/dualface/kander/rules"
)

// The archive preserves the four changed official rule files byte-for-byte from
// b8aef0e68e430b23d55496858ce11bede14a8bb8. It is a historical test fixture, not
// another installed rules copy. Keeping real old bytes exercises the hash lookup
// without replacing the registry or depending on a checkout's Git history.
func TestUpgradeUnstampedFourRoleRules(t *testing.T) {
	archive, err := zip.OpenReader(filepath.Join("..", "testdata", "rules", "pre-two-role-rules.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	old := make(map[string][]byte)
	for _, file := range archive.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: %v; close: %v", file.Name, readErr, closeErr)
		}
		old[file.Name] = data
	}
	wantOutdated := []string{"KANDER-BASE-RULES.md", "KANDER-KANBAN-RULES.md", "KANDER-REPORTING-RULES.md", "KANDER-REVIEW-RULES.md"}
	gotNames := make([]string, 0, len(old))
	for name := range old {
		gotNames = append(gotNames, name)
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantOutdated) {
		t.Fatalf("fixture names=%v", gotNames)
	}
	for _, edited := range []bool{false, true} {
		name := "official"
		if edited {
			name = "locally-edited"
		}
		t.Run(name, func(t *testing.T) {
			paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: t.TempDir()}
			for _, name := range rules.Names() {
				data, err := rules.File(name)
				if err != nil {
					t.Fatal(err)
				}
				if legacy, ok := old[name]; ok {
					data = bytes.Clone(legacy)
					if edited {
						data = append(data, []byte("\nLocal policy adjustment.\n")...)
					}
				}
				if err := os.WriteFile(filepath.Join(paths.RulesDir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(paths.RulesDir, "kander-rules-state.json")); !os.IsNotExist(err) {
				t.Fatalf("fixture unexpectedly stamped: %v", err)
			}
			report, err := install.InspectRules(paths)
			if err != nil {
				t.Fatal(err)
			}
			if edited {
				if !reflect.DeepEqual(report.Modified, wantOutdated) || len(report.Outdated) != 0 {
					t.Fatalf("edited rules classified incorrectly: %+v", report)
				}
			} else if !reflect.DeepEqual(report.Outdated, wantOutdated) || len(report.Modified) != 0 {
				t.Fatalf("official rules classified incorrectly: %+v", report)
			}
			if len(report.Missing) != 0 {
				t.Fatalf("missing rules: %v", report.Missing)
			}
			if _, _, err := install.RepairRules(paths); err != nil {
				t.Fatal(err)
			}
			for _, name := range wantOutdated {
				want, err := rules.File(name)
				if err != nil {
					t.Fatal(err)
				}
				if edited {
					want = append(bytes.Clone(old[name]), []byte("\nLocal policy adjustment.\n")...)
				}
				got, err := os.ReadFile(filepath.Join(paths.RulesDir, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("repair %s: err=%v; content mismatch=%v", name, err, !bytes.Equal(got, want))
				}
			}
			if _, _, err := install.RepairRules(paths); err != nil {
				t.Fatalf("repeat repair: %v", err)
			}
			after, err := install.InspectRules(paths)
			if err != nil || len(after.Missing)+len(after.Outdated) != 0 {
				t.Fatalf("after repair: %+v, %v", after, err)
			}
			if (!edited && len(after.Modified) != 0) || (edited && !reflect.DeepEqual(after.Modified, wantOutdated)) {
				t.Fatalf("after repair modifications: %+v", after)
			}
		})
	}
}
