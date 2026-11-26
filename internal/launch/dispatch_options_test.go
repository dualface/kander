package launch

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command/commandtest"
)

func TestDispatchFlagsAreGivenOnceAndNeverEmpty(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want int
	}{
		{"all", []string{"--dispatch-id", "d1", "--kind=fix", "--base", "abc", "--evidence-file", "e.json"}, 0},
		{"value that looks like a flag", []string{"--base", "--kind=sync", "--kind", "fix"}, 0},
		{"repeated", []string{"--kind", "fix", "--kind=sync"}, 2},
		{"empty", []string{"--dispatch-id="}, 2},
		{"missing value", []string{"--evidence-file"}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var options DispatchOptions
			c := &cobra.Command{Use: "demo", RunE: func(*cobra.Command, []string) error { return nil }}
			AddDispatchFlags(c, &options)
			if code := commandtest.Run(c, test.args...); code != test.want {
				t.Fatalf("code=%d want=%d", code, test.want)
			}
			if test.name == "value that looks like a flag" && (options.Base != "--kind=sync" || options.Kind != "fix") {
				t.Fatalf("options=%+v", options)
			}
		})
	}
}
