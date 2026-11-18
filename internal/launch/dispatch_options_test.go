package launch

import (
	"reflect"
	"testing"
)

func TestDispatchOptionsPreserveOtherOptionValues(t *testing.T) {
	for _, option := range []string{"--message", "--message-file", "--timeout", "--agent", "--launcher", "--pane"} {
		for _, value := range []string{"--kind=sync", "--dispatch-id", "--base=literal"} {
			t.Run(option+"/"+value, func(t *testing.T) {
				original := []string{"task", option, value}
				args := append(append([]string{}, original...), "--kind", "fix")
				rest, options, err := ParseDispatchOptions(args)
				if err != nil || !reflect.DeepEqual(rest, original) || options != (DispatchOptions{Kind: "fix"}) {
					t.Fatalf("literal argument reinterpreted: rest=%q options=%+v err=%v", rest, options, err)
				}
			})
		}
	}
}
