package review

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

func TestEvidenceLimitsFitArtifactJSON(t *testing.T) {
	if !(evidencePreviewContext < evidencePreviewLine && evidencePreviewLine < evidenceUnitPreview &&
		evidenceUnitPreview < evidenceFileLimit && evidenceFileLimit < evidenceTotalLimit && evidenceTotalLimit <= evidenceHardLimit) {
		t.Fatal("evidence limits are not ordered")
	}
	for _, size := range []int{evidenceTotalLimit, evidenceHardLimit} {
		data := make([]byte, size)
		body, err := json.Marshal(api.ReviewArtifact{Name: "evidence.txt", Data: data, Digest: board.ReviewDigest(data)})
		if err != nil || len(body) > artifactRequestLimit {
			t.Fatalf("%d evidence bytes encode to %d: %v", size, len(body), err)
		}
	}
}

func testEvidenceLimits() evidenceLimits {
	return evidenceLimits{file: evidenceFileLimit, total: evidenceTotalLimit, hard: evidenceHardLimit}
}

func testEvidencePatch(name, body string) string {
	return "diff --git a/" + name + " b/" + name + "\n@@ -1 +1 @@\n" + body
}

func testEvidenceUnits(section string, names ...string) *evidencePatch {
	patch := &evidencePatch{from: strings.Repeat("a", 40), to: strings.Repeat("b", 40)}
	for _, path := range names {
		block := testEvidencePatch(path, "-"+strings.Repeat("a", 2000)+"\n+"+strings.Repeat("b", 2000)+"\n")
		unit := newEvidenceDiff(patch, section, []string{block}, path, true)
		unit.metadata = &evidenceMetadata{path: path, status: "M", numstat: "1\t1"}
		patch.units = append(patch.units, unit)
	}
	return patch
}

func TestEvidenceTotalBudgetOrder(t *testing.T) {
	patch := testEvidenceUnits("PATCH", "z", "b", "a")
	fix := testEvidenceUnits("FIX RANGE PATCH", "a")
	// Equalize the first three full sizes, deliberately list FIX first, and
	// let the longest original win even though its path sorts last.
	for i := range patch.units {
		unit := &patch.units[i]
		unit.blocks[0] = testEvidencePatch(unit.path, "-"+strings.Repeat("a", 8000)+"\n")
		if unit.path == "z" {
			unit.blocks[0] += "+extra\n"
		}
		unit.fullBytes = len(unit.blocks[0])
	}
	unit := &fix.units[0]
	unit.blocks[0] = testEvidencePatch(unit.path, "-"+strings.Repeat("a", 8000)+"\n")
	unit.fullBytes = len(unit.blocks[0])

	tinyPatch := testEvidenceUnits("PATCH", "tiny")
	tiny := &tinyPatch.units[0]
	tiny.blocks[0] = testEvidencePatch(tiny.path, "-a\n+b\n")
	tiny.fullBytes = len(tiny.blocks[0])
	inputs := []evidenceSection{{name: "FIX RANGE PATCH", patch: fix}, {name: "PATCH", patch: patch}, {name: "PATCH", patch: tinyPatch}}
	// Two previews are enough. The larger z and then PATCH a must be the picks.
	const target = 22500
	body, result := renderEvidence("range\n", inputs, evidenceLimits{file: 10000, total: target, hard: target})
	if result.omitted != 2 || len(body) > target {
		t.Fatalf("omitted %d, %d bytes", result.omitted, len(body))
	}
	for _, check := range []struct {
		unit *evidenceDiff
		want bool
	}{{&patch.units[0], true}, {&patch.units[1], false}, {&patch.units[2], true}, {&fix.units[0], false}, {tiny, false}} {
		if got := strings.Contains(check.unit.replacement.item, "shown: preview"); got != check.want {
			t.Errorf("preview of %s %s = %v, want %v", check.unit.section, check.unit.path, got, check.want)
		}
	}
}

func TestEvidenceTargetAndHeaderDowngrade(t *testing.T) {
	for _, test := range []struct {
		name        string
		total, hard int
		want        int
		header      bool
	}{
		{"soft", 6500, 12000, 2, true},
		{"hard", 5200, 6600, 2, true},
		{"minimum exceeds hard", 4800, 5000, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			patch := testEvidenceUnits("PATCH", "a", "b")
			// Only a is individually oversized. Both need omission to reach
			// either achievable limit because of the fixed 4000-byte section.
			patch.units[0].blocks[0] += strings.Repeat("x", 4000) + "\n"
			patch.units[0].fullBytes = len(patch.units[0].blocks[0])
			sections := []evidenceSection{{name: "COMMITS", text: strings.Repeat("c", 4000)}, {name: "PATCH", patch: patch}}
			out, result := renderEvidence("", sections, evidenceLimits{file: 5000, total: test.total, hard: test.hard})
			if result.omitted != test.want {
				t.Fatalf("omitted %d, want %d (%d bytes)", result.omitted, test.want, len(out))
			}
			shown := patch.units[0].replacement
			if got := strings.Contains(shown.item, "shown: header-only"); got != test.header {
				t.Fatalf("header-only = %v, want %v (%d bytes)", got, test.header, len(out))
			}
			if test.header {
				if strings.Count(shown.body, "\n") != 2 || !strings.Contains(shown.body, "per-file limit") {
					t.Fatalf("not header and marker only: %q", shown.body)
				}
				if len(out) > test.hard || test.name == "soft" && len(out) > test.total {
					t.Fatalf("achievable target not met: %d", len(out))
				}
			} else if patch.units[1].replacement.body != "" || !strings.Contains(shown.item, "shown: preview") {
				t.Fatal("an impossible budget changed the per-file result")
			}
		})
	}
}

func TestEvidenceManyMediumDiffsReachUploadBudget(t *testing.T) {
	patch := &evidencePatch{from: strings.Repeat("a", 40), to: strings.Repeat("b", 40)}
	const count = 750
	body := strings.Repeat("+ "+strings.Repeat("x", 96)+"\n", 405)
	for i := range count {
		name := fmt.Sprintf("medium-%03d.txt", i)
		block := testEvidencePatch(name, body)
		unit := newEvidenceDiff(patch, "PATCH", []string{block}, name, true)
		unit.metadata = &evidenceMetadata{path: name, status: "M", numstat: "405\t0"}
		patch.units = append(patch.units, unit)
	}
	sections := []evidenceSection{{name: "PATCH", patch: patch}}
	out, result := renderEvidence("range\n", sections, testEvidenceLimits())
	if result.omitted == 0 || result.omitted == count || len(out) > evidenceTotalLimit {
		t.Fatalf("omitted %d/%d, %d bytes", result.omitted, count, len(out))
	}
	if strings.Contains(out, "shown: preview") || strings.Count(out, "shown: header-only") != result.omitted {
		t.Fatal("short-line patches should go straight to headers when their preview costs more")
	}
}

func TestEvidencePreviewShowsChangedByteAndCapsOutput(t *testing.T) {
	before := strings.Repeat("界", 1000)
	after := strings.Repeat("後", 1000)
	hunk := "@@ -1 +1 @@\n-" + before + "LEFT" + after + "\n+" + before + "RIGHT" + after + "\n"
	patch := "diff --git a/long b/long\nindex a..b 100644\n" + hunk + strings.Repeat(hunk, 40)
	got := previewEvidenceDiff([]string{patch})
	if len(got) > evidenceUnitPreview || !strings.Contains(got, "further hunk header(s)") ||
		!strings.Contains(got, "original bytes not shown") || !utf8.ValidString(got) {
		t.Fatal("preview exceeds its budget, lacks remainder counts, or splits UTF-8")
	}
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[2], "-[2742 leading bytes skipped] ") || !strings.Contains(lines[2], "LEFT") ||
		!strings.HasPrefix(lines[3], "+[2742 leading bytes skipped] ") || !strings.Contains(lines[3], "RIGHT") {
		t.Fatalf("long lines miss their first changed byte: %q and %q", lines[2], lines[3])
	}
	for _, line := range lines {
		if len(line)+1 > evidencePreviewLine || strings.HasPrefix(line, "diff --git ") {
			t.Fatalf("invalid preview line of %d bytes", len(line))
		}
	}
}

func TestEvidenceFallbackPathPriority(t *testing.T) {
	for _, tc := range []struct {
		name, block, path, old string
		parsed                 bool
	}{
		{"rename header", "diff --git a/old b/new\nrename from old\nrename to new\n+++ b/ignored\n", "new", "old", true},
		{"copy header", "diff --git a/old b/new\ncopy from old\ncopy to new\n", "new", "old", true},
		{"spaced new", "diff --git a/with space b/with space\n+++ b/with space\t\n", "with space", "", true},
		{"spaced deletion", "diff --git a/with space b/with space\n--- a/with space\t\n+++ /dev/null\n", "with space", "", true},
		{"quoted header", "diff --git \"a/\\303\\251\" \"b/\\303\\251\"\n", "é", "", true},
		{"ambiguous", "diff --git a/one b/two\n", "diff --git a/one b/two", "", false},
		{"colored ambiguous", "\x1b[1mdiff --git a/one b/two\x1b[m\n", "\x1b[1mdiff --git a/one b/two\x1b[m", "", false},
		{"invalid UTF-8", "diff --git \"a/\\377\" \"b/\\377\"\n", "\xff", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, old, parsed := fallbackEvidencePath(tc.block)
			if path != tc.path || old != tc.old || parsed != tc.parsed {
				t.Fatalf("got (%q, %q, %v)", path, old, parsed)
			}
		})
	}
}

func TestEvidencePathEncoding(t *testing.T) {
	var out strings.Builder
	writeEvidencePath(&out, "path", "<猫>&")
	writeEvidencePath(&out, "old_path", "bad\xff")
	lines := strings.Split(out.String(), "\n")
	if lines[0] != `  path: "<猫>&"` || lines[1] != `  old_path: "bad�"` {
		t.Fatalf("not encoding/json path strings: %q", lines)
	}
	encoded, ok := strings.CutPrefix(lines[2], "  old_path_base64: ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if !ok || err != nil || string(decoded) != "bad\xff" || len(lines) != 4 {
		t.Fatalf("invalid appendix: %q (%v)", lines[2], err)
	}
}

func writeTestEvidence(t *testing.T, ctx reviewContext) (string, evidenceResult) {
	t.Helper()
	runtime := t.TempDir()
	path := filepath.Join(runtime, "evidence.txt")
	result, err := writeEvidence(ctx, runtime, path)
	if err != nil {
		t.Fatal(err)
	}
	return readFile(t, path), result
}

func TestEvidenceOversizedPromptSuggestsShorterContext(t *testing.T) {
	root := t.TempDir()
	setupLang(t, filepath.Join(root, "config.json"))
	size := artifactRequestLimit / 4 * 3
	if err := os.WriteFile(filepath.Join(root, "prompt.txt"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	// An accidental upload would try to use the uninitialized session.
	archive := archiveExecution{store: &remoteReviewStore{}}
	err := archive.snapshotRuntime(root, evidenceResult{})
	var gate *gateError
	if !errors.As(err, &gate) || gate.code != 2 {
		t.Fatalf("prompt limit did not fail as a gate error: %v", err)
	}
	message := gate.Error()
	for _, want := range []string{"prompt.txt", "Shorten supplemental review context", "new run ID"} {
		if !strings.Contains(message, want) {
			t.Errorf("prompt limit message does not include %q: %s", want, message)
		}
	}
	if strings.Contains(message, ".gitattributes") {
		t.Errorf("prompt limit message gives unrelated evidence guidance: %s", message)
	}
	// review-contract.md is the other non-evidence artifact. Its advice also
	// permits shortening custom instructions rather than changing Git config.
	contract := config.Text("review.artifact_request_too_large", config.ReviewContractFilename, size, artifactRequestLimit+1, artifactRequestLimit, "")
	if !strings.Contains(contract, "custom review instructions") || strings.Contains(contract, ".gitattributes") {
		t.Errorf("wrong contract limit guidance: %s", contract)
	}
}
