package review

import (
	"strings"
	"testing"
)

func TestIntegratedPromptOrderAndOriginalRoles(t *testing.T) {
	for _, role := range []string{"PM", "QA", "CSA", "Hacker", "PMQA", "Security"} {
		if roleRules[role] == "" {
			t.Fatal("missing role prompt", role)
		}
	}
	pmqa := roleRules["PMQA"]
	contract, quality := strings.Index(pmqa, "Build a requirement table"), strings.Index(pmqa, "Act as the quality owner")
	if contract < 0 || quality <= contract || !strings.Contains(pmqa, "PM component owns explicit performance acceptance") || !strings.Contains(pmqa, "root cause once") {
		t.Fatal("PMQA must perform contract checks before quality checks without duplicate findings")
	}
	security := roleRules["Security"]
	boundary, attacker := strings.Index(security, "Trace untrusted inputs across trust boundaries"), strings.Index(security, "Act as an external attacker")
	if boundary < 0 || attacker <= boundary || !strings.Contains(security, "root cause only once") || !strings.Contains(security, "no qualifying exploit chain exists") {
		t.Fatal("Security must analyze trust boundaries before deduplicated attack chains")
	}
}
