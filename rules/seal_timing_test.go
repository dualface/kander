package rules

import (
	"strings"
	"testing"
)

// The seal timing rule of a single card must keep the explicit N/A plan, which
// is sealed at creation, out of its "create unsealed" instruction: a card that
// followed it with review off could not complete (unsealed-plan).
func TestSingleCardSealTimingKeepsNAPlansSealed(t *testing.T) {
	data, err := File("snippets/execute-single.md")
	if err != nil {
		t.Fatal(err)
	}
	var rule string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "review plan unsealed") {
			if rule != "" {
				t.Fatal("more than one seal timing rule")
			}
			rule = line
		}
	}
	for _, want := range []string{"When a review role runs, create the card's review plan unsealed", "the explicit N/A plan of `snippets/review-evidence-gate.md` stays sealed at creation", "`append-cycle-batch`", "`seal` omitted or `false`"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("seal timing rule lacks %q:\n%s", want, rule)
		}
	}
	gate, err := File("snippets/review-evidence-gate.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gate), "\"sealed\": true,") {
		t.Fatal("the explicit N/A plan template is no longer sealed at creation")
	}
}
