package review

import (
	"strings"
	"testing"
)

func TestReviewRolePromptsAreIndependent(t *testing.T) {
	if _, ok := roleRules["PM"]; ok || roleRules["QA"] != "" || roleRules["CSA"] != "" || roleRules["Hacker"] != "" {
		t.Fatal("deleted roles must not remain in roleRules")
	}
	pmqa := roleRules["PMQA"]
	contract, quality := strings.Index(pmqa, "Build a requirement table"), strings.Index(pmqa, "Act as the quality owner")
	if contract < 0 || quality <= contract || !strings.Contains(pmqa, "PM component owns explicit performance acceptance") || !strings.Contains(pmqa, "root cause once") || !strings.Contains(pmqa, "PMQA-prefixed finding IDs") {
		t.Fatal("PMQA must cover original PM+QA duties without losing contract-before-quality order")
	}
	if !strings.Contains(pmqa, "1000 physical lines") || !strings.Contains(pmqa, "requirement table is analysis") {
		t.Fatal("PMQA dropped original PM or QA clauses")
	}
	security := roleRules["Security"]
	boundary, attacker := strings.Index(security, "Trace untrusted inputs across trust boundaries"), strings.Index(security, "Act as an external attacker")
	if boundary < 0 || attacker <= boundary || !strings.Contains(security, "root cause only once") || !strings.Contains(security, "no qualifying exploit chain exists") || !strings.Contains(security, "Security-prefixed finding IDs") {
		t.Fatal("Security must cover original CSA+Hacker duties without losing boundary-before-chains order")
	}
}

func TestCanonicalizeReviewRoleAcceptsOnlyCurrentRoles(t *testing.T) {
	for _, input := range []string{"PMQA", "pmqa", "PmQa", "Security", "security", "SECURITY"} {
		role, ok := canonicalizeReviewRole(input)
		if !ok {
			t.Fatalf("rejected %q", input)
		}
		if input != "" && (role != "PMQA" && role != "Security") {
			t.Fatalf("%q -> %q", input, role)
		}
	}
	for _, input := range []string{"PM", "QA", "CSA", "Hacker", "CodeSecurityAnalyst", "pm", "qa", "csa", "hacker", "codesecurityanalyst"} {
		if role, ok := canonicalizeReviewRole(input); ok {
			t.Fatalf("accepted deleted role %q as %q", input, role)
		}
	}
}
