package review

import (
	"strings"
	"testing"
)

func TestIntegratedReviewRolePromptsComposeHistoricalRoles(t *testing.T) {
	pmqa := roleRules["PMQA"]
	contract, quality := strings.Index(pmqa, "Build a requirement table"), strings.Index(pmqa, "Act as the quality owner")
	if contract < 0 || quality <= contract || !strings.Contains(pmqa, "PM component owns explicit performance acceptance") || !strings.Contains(pmqa, "root cause once") || !strings.Contains(pmqa, "PMQA-prefixed finding IDs") {
		t.Fatal("PMQA must cover original PM+QA duties without losing contract-before-quality order")
	}
	if !strings.Contains(pmqa, "1000 physical lines") || !strings.Contains(pmqa, "requirement table is analysis") {
		t.Fatal("PMQA dropped original PM or QA clauses")
	}
	if !strings.Contains(pmqa, strings.TrimSpace(roleRules["PM"])) || !strings.Contains(pmqa, strings.TrimSpace(roleRules["QA"])) {
		t.Fatal("PMQA must compose the shared PM and QA role resources")
	}
	security := roleRules["Security"]
	boundary, attacker := strings.Index(security, "Trace untrusted inputs across trust boundaries"), strings.Index(security, "Act as an external attacker")
	if boundary < 0 || attacker <= boundary || !strings.Contains(security, "root cause only once") || !strings.Contains(security, "no qualifying exploit chain exists") || !strings.Contains(security, "Security-prefixed finding IDs") {
		t.Fatal("Security must cover original CSA+Hacker duties without losing boundary-before-chains order")
	}
	if roleRules["PM"] == "" || roleRules["QA"] == "" || roleRules["CSA"] == "" || roleRules["Hacker"] == "" {
		t.Fatal("historical roles need prompts for open four-key batches")
	}
	if !strings.Contains(security, strings.TrimSpace(roleRules["CSA"])) || !strings.Contains(security, strings.TrimSpace(roleRules["Hacker"])) {
		t.Fatal("Security must compose the shared CSA and Hacker role resources")
	}
}

func TestParseReviewRoleAcceptsCurrentAndHistoricalNames(t *testing.T) {
	for _, input := range []string{"PMQA", "pmqa", "PmQa", "Security", "security", "SECURITY"} {
		role, ok := parseReviewRole(input)
		if !ok || !currentReviewRole(role) {
			t.Fatalf("rejected current role %q as %q", input, role)
		}
	}
	got, ok := parseReviewRole("CodeSecurityAnalyst")
	if !ok || got != "CSA" {
		t.Fatalf("CodeSecurityAnalyst -> %q %v", got, ok)
	}
	for _, input := range []string{"PM", "QA", "CSA", "Hacker", "pm", "qa", "csa", "hacker"} {
		role, ok := parseReviewRole(input)
		if !ok || currentReviewRole(role) {
			t.Fatalf("historical %q -> %q %v", input, role, ok)
		}
	}
	if _, ok := parseReviewRole("Owner"); ok {
		t.Fatal("accepted unknown role")
	}
}
