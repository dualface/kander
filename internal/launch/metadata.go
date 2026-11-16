package launch

import (
	"strconv"
	"strings"
	"unicode"

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

// tabTitleLimit is the longest display title of a container, in runes.
const tabTitleLimit = 40

// taskTabTitle is the display title of a card launch: the card title, with
// control characters removed and cut to tabTitleLimit runes with a trailing
// ellipsis. It is empty for a card without a title, so the container label
// stays in use.
func taskTabTitle(text string) string {
	title := board.TitleFrom(text)
	if title == t("board.untitled") {
		return ""
	}
	return capTabTitle(title)
}

// orchestratorTabTitle is the display title of an orchestrator launch: the ID
// of the first reference, followed by the count of the others. References are
// the user's arguments, so a task group is not expanded into its members.
func orchestratorTabTitle(references []string) string {
	if len(references) == 0 {
		return ""
	}
	title := references[0]
	if rest := len(references) - 1; rest > 0 {
		title += " +" + strconv.Itoa(rest)
	}
	return capTabTitle(title)
}

func capTabTitle(title string) string {
	runes := []rune(strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)))
	if len(runes) <= tabTitleLimit {
		return string(runes)
	}
	return string(runes[:tabTitleLimit-1]) + "…"
}
