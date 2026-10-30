package launch

import (
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/terminal"
)

func metadataFrom(text, name string) string {
	return board.MetadataFrom(text, name)
}

func windowName(entry board.Entry, text string) string {
	title := board.TitleFrom(text)
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		if r < 32 {
			continue
		}
		if r == ' ' || r == '\t' {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		b.WriteRune(r)
		lastDash = false
	}
	label := strings.Trim(b.String(), "-")
	untitled := t("board.untitled")
	if label == "" || title == untitled {
		slug := strings.TrimSuffix(entry.TaskID[9:], "-task")
		label = slug
	}
	name := "kb-" + label
	runes := []rune(name)
	if len(runes) > 50 {
		name = string(runes[:50])
	}
	return name
}

func taskGroupFrom(text string) string {
	return board.TaskGroupFrom(text)
}

func locationOf(plan LaunchPlan, outcome LaunchOutcome) string {
	return terminal.FormatAddress(plan.backend(), plan.address(outcome))
}

// recordWindowLocation writes WINDOW once the container exists, together with the
// EXEC_RESOLVED record of a relaunch whose card pins anything.
func recordWindowLocation(root string, plan LaunchPlan, entry board.Entry, choice execChoice) func(LaunchOutcome) error {
	return func(outcome LaunchOutcome) error {
		_, err := recordWindowFn(root, entry, locationOf(plan, outcome), choice.record)
		return err
	}
}
