package review

import (
	"errors"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
)

// --requirement-decision takes ROLE=ID once per role, for PMQA or Security,
// and only with --requirements-file, which creates the batch it justifies.
func TestRequirementDecisionFlag(t *testing.T) {
	parse := func(args ...string) (archiveOptions, error) {
		var flags archiveFlags
		c := &cobra.Command{Use: "review"}
		flags.register(c)
		if err := c.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		return flags.options(c)
	}
	options, err := parse("--task", "20261127-demo-task", "--batch-id", "batch", "--requirements-file", "r.json",
		"--requirement-decision", "PMQA=decision-1", "--requirement-decision", "Security=decision-2")
	if err != nil || !reflect.DeepEqual(options.decisions, map[string]string{"PMQA": "decision-1", "Security": "decision-2"}) {
		t.Fatalf("valid decisions: %+v %v", options.decisions, err)
	}
	for name, args := range map[string][]string{
		"no requirements file": {"--task", "20261127-demo-task", "--batch-id", "batch", "--requirement-decision", "PMQA=d"},
		"unknown role":         {"--task", "20261127-demo-task", "--batch-id", "batch", "--requirements-file", "r.json", "--requirement-decision", "QA=d"},
		"repeated role":        {"--task", "20261127-demo-task", "--batch-id", "batch", "--requirements-file", "r.json", "--requirement-decision", "PMQA=a", "--requirement-decision", "PMQA=b"},
		"no ID":                {"--task", "20261127-demo-task", "--batch-id", "batch", "--requirements-file", "r.json", "--requirement-decision", "PMQA="},
		"no separator":         {"--task", "20261127-demo-task", "--batch-id", "batch", "--requirements-file", "r.json", "--requirement-decision", "PMQA"},
		"no task":              {"--requirement-decision", "PMQA=d"},
	} {
		var usage *command.UsageError
		if _, err := parse(args...); !errors.As(err, &usage) {
			t.Fatalf("%s: want a usage error, got %v", name, err)
		}
	}
}
