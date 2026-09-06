package launch

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/window"
)

func metadataFrom(text, name string) string {
	return board.MetadataFrom(text, name)
}

func quoteReplacement(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}

func replaceLiteral(pattern *regexp.Regexp, text, repl string) string {
	return pattern.ReplaceAllString(text, quoteReplacement(repl))
}

func replaceUniqueField(text, name, value string) (string, error) {
	pattern := board.FieldLineRe(name)
	if len(pattern.FindAllStringIndex(text, -1)) != 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", name,
		)
	}
	return replaceLiteral(pattern, text, board.RenderField(name, value)), nil
}

// insertAfterField appends a new metadata line right below an existing one,
// which must be present exactly once.
func insertAfterField(text, anchor, name, value string) (string, error) {
	pattern := board.FieldLineRe(anchor)
	lines := pattern.FindAllString(text, -1)
	if len(lines) != 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", anchor,
		)
	}
	return replaceLiteral(pattern, text, lines[0]+"\n"+board.RenderField(name, value)), nil
}

func renderStartMetadata(text, agent, session, window string) (string, error) {
	for _, pair := range [][2]string{
		{board.FieldOwner, agent},
		{board.FieldStartedAt, nowStamp()},
	} {
		pattern := regexp.MustCompile(`(?m)^- ` + board.TokenPattern(pair[0]) + `:\s*$`)
		if len(pattern.FindAllStringIndex(text, -1)) != 1 {
			return "", launchError(
				"launch.task_document_must_contain_exactly_one_metadata_field", pair[0],
			)
		}
		text = replaceLiteral(pattern, text, board.RenderField(pair[0], pair[1]))
	}
	sessionRe := board.FieldLineRe(sessionField)
	sessionLines := sessionRe.FindAllString(text, -1)
	if len(sessionLines) > 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", sessionField,
		)
	}
	var err error
	if len(sessionLines) > 0 {
		text = replaceLiteral(sessionRe, text, board.RenderField(sessionField, session))
	} else {
		text, err = insertAfterField(text, board.FieldOwner, sessionField, session)
		if err != nil {
			return "", err
		}
	}
	windowRe := board.FieldLineRe(windowField)
	windowLines := windowRe.FindAllString(text, -1)
	if len(windowLines) > 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", windowField,
		)
	}
	if len(windowLines) > 0 {
		text = regexp.MustCompile(`(?m)^- `+board.TokenPattern(windowField)+`:.*\n?`).ReplaceAllString(text, "")
	}
	return insertAfterField(text, sessionField, windowField, window)
}

func renderTakeoverMetadata(text, agent, session, window string) (string, error) {
	var err error
	text, err = replaceUniqueField(text, board.FieldOwner, agent)
	if err != nil {
		return "", err
	}
	text, err = replaceUniqueField(text, sessionField, session)
	if err != nil {
		return "", err
	}
	windowLines := board.FieldLineRe(windowField).FindAllString(text, -1)
	if len(windowLines) > 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", windowField,
		)
	}
	if len(windowLines) > 0 {
		return replaceUniqueField(text, windowField, window)
	}
	return insertAfterField(text, sessionField, windowField, window)
}

func renderSessionMetadata(text, session string) (string, error) {
	return replaceUniqueField(text, sessionField, session)
}

func startMetadata(text, agent string, session AgentSession, window string) (string, error) {
	return renderStartMetadata(text, agent, session.Render(), window)
}

func windowMetadata(text, value string) (string, error) {
	return window.RenderWindowMetadata(text, value)
}

func renameEntry(root string, entry board.Entry, targetState string) (board.Entry, error) {
	target := filepath.Join(root, targetState, filepath.Base(entry.Path))
	err := fs.Rename(root, entry.Path, target)
	if err != nil {
		return board.Entry{}, launchError("board.move_failed", err.Error())
	}
	document := target
	if entry.Kind == "large" {
		document = filepath.Join(target, "spec.md")
	}
	return board.Entry{TaskID: entry.TaskID, State: targetState, Path: target, Document: document, Kind: entry.Kind}, nil
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
	if plan.Launcher == "herdr" {
		return "herdr:" + outcome.Tab + ":" + outcome.Pane
	}
	return plan.Launcher + ":" + plan.Session + ":" + outcome.Window + ":" + outcome.Pane
}

func recordWindowLocation(root string, plan LaunchPlan, entry board.Entry) func(LaunchOutcome) error {
	return func(outcome LaunchOutcome) error {
		current, err := readDocumentFn(entry)
		if err != nil {
			return err
		}
		updated, err := windowMetadata(current, locationOf(plan, outcome))
		if err != nil {
			return err
		}
		return writeDocumentFn(root, entry, updated)
	}
}
