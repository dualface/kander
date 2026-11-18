package board

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/rules"
)

// The Output section of the review contract defines every field of the evidence results with
// its type. Each JSON field of the result types is named there, so a field added to a result
// without its definition fails here.
func TestReviewContractDefinesTheResultFields(t *testing.T) {
	data, err := rules.File("snippets/commands/review.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := strings.Index(text, "## Output"), strings.Index(text, "## Rejections")
	if start < 0 || end < start {
		t.Fatal("the review contract has no Output section")
	}
	output := text[start:end]
	for _, value := range []any{ReviewProgress{}, ReviewClosure{}, ReviewBatchView{}, ReviewNextAction{}, ReviewExclusion{}, ReviewBatchResolution{}, ReviewCloseRequest{}, ReviewGitEvidence{}, ReviewBatch{}, ReviewRunView{}} {
		kind := reflect.TypeOf(value)
		for i := range kind.NumField() {
			name, _, _ := strings.Cut(kind.Field(i).Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			if !strings.Contains(output, "`"+name+"`") {
				t.Errorf("%s.%s: the Output section of review.md does not define `%s`", kind.Name(), kind.Field(i).Name, name)
			}
		}
	}
}
