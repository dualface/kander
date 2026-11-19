package board

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/rules"
)

// The Output section of the review contract defines every object of the evidence results on a
// line of its own, that starts with the path of the object, and defines each field there with
// its type: "`name` (type...". A field defined on another object's line, or without a type, does
// not count, so a nested object cannot be described by the wrong array.
var reviewOutputObjects = []struct {
	label string
	types []any
}{
	{"aggregate", []any{ReviewBatchView{}}},
	{"aggregate.batch", []any{ReviewBatch{}}},
	{"aggregate.batch.advances[]", []any{ReviewAdvance{}}},
	{"aggregate.runs[]", []any{ReviewRunView{}}},
	{"aggregate.runs[].assignment", []any{ReviewAssignment{}}},
	{"aggregate.runs[].run", []any{ReviewRun{}, ReviewInput{}}},
	{"close", []any{ReviewClosure{}}},
	{"close.request", []any{ReviewCloseRequest{}}},
	{"close.request.roles{}", []any{ReviewRoleConclusion{}}},
	{"close.request.superseded_runs{}", []any{ReviewSupersededRun{}}},
	{"close.request.mechanical[]", []any{ReviewMechanicalAssessment{}, FindingRef{}}},
	{"close.request.opinions[]", []any{ReviewOpinion{}}},
	{"close.git", []any{ReviewGitEvidence{}}},
	{"close.git.edges[]", []any{ReviewGitEdge{}}},
	{"close.git.mechanical[]", []any{ReviewMechanicalGit{}}},
	{"progress", []any{ReviewProgress{}}},
	{"progress.exclusion", []any{ReviewExclusion{}}},
	{"progress.next_actions[]", []any{ReviewNextAction{}}},
	{"progress.resolved_batches[]", []any{ReviewBatchResolution{}}},
	{"progress.resolved_batches[].request", []any{ReviewBatchResolutionRequest{}}},
	{"progress.resolved_batches[].request.findings[]", []any{ReviewResolvedFinding{}}},
	{"progress.resolved_batches[].runs[]", []any{ReviewResolvedRun{}}},
	{"progress.resolved_batches[].git", []any{ReviewResolutionGit{}}},
}

func TestReviewContractDefinesEveryResultObjectWithTypes(t *testing.T) {
	data, err := rules.File("snippets/commands/review.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := strings.Index(text, "## Output"), strings.Index(text, "## Rejections")
	if start < 0 || end < start {
		t.Fatal("the review contract has no Output section")
	}
	lines := strings.Split(text[start:end], "\n")
	for _, object := range reviewOutputObjects {
		prefix := "  - `" + object.label + "` ("
		var line string
		for _, candidate := range lines {
			if strings.HasPrefix(candidate, prefix) {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Errorf("the Output section of review.md has no line for the object `%s`", object.label)
			continue
		}
		for _, value := range object.types {
			kind := reflect.TypeOf(value)
			for i := range kind.NumField() {
				name, _, _ := strings.Cut(kind.Field(i).Tag.Get("json"), ",")
				if name == "" || name == "-" {
					continue
				}
				if !strings.Contains(line, "`"+name+"` (") {
					t.Errorf("%s.%s: the line of `%s` does not define `%s` with its type", kind.Name(), kind.Field(i).Name, object.label, name)
				}
			}
		}
	}
}

// The role of a run is any of the six roles a batch can hold: PMQA and Security for new runs, and
// PM, QA, CSA and Hacker for the runs of historical batches.
func TestReviewContractNamesEveryRunRole(t *testing.T) {
	data, err := rules.File("snippets/commands/review.md")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, candidate := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(candidate, "  - `aggregate.runs[].run` (") {
			line = candidate
		}
	}
	for _, role := range []string{"PMQA", "Security", "PM", "QA", "CSA", "Hacker"} {
		if !strings.Contains(line, "`"+role+"`") {
			t.Errorf("the line of aggregate.runs[].run does not name the role `%s`", role)
		}
	}
	if strings.Contains(string(data), "the record the `assign` form printed") {
		t.Error("review.md still says that assign prints the saved assignment")
	}
}
