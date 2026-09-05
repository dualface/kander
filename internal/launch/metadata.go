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

func replaceKeep1(pattern *regexp.Regexp, text, suffix string) string {
	return pattern.ReplaceAllString(text, "${1}"+quoteReplacement(suffix))
}

func replaceUniqueField(text, name, value string) (string, error) {
	pattern := regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(name) + `:.*$`)
	if len(pattern.FindAllStringIndex(text, -1)) != 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", name,
		)
	}
	return replaceLiteral(pattern, text, "- "+name+": "+strings.TrimRight(value, " ")), nil
}

func renderStartMetadata(text, agent, session, window string) (string, error) {
	for _, pair := range [][2]string{
		{"负责人", agent},
		{"开始时间", nowStamp()},
	} {
		pattern := regexp.MustCompile(`(?m)^- ` + pair[0] + `:\s*$`)
		if len(pattern.FindAllStringIndex(text, -1)) != 1 {
			return "", launchError(
				"launch.task_document_must_contain_exactly_one_metadata_field", pair[0],
			)
		}
		text = replaceLiteral(pattern, text, "- "+pair[0]+": "+pair[1])
	}
	sessionLines := regexp.MustCompile(`(?m)^- `+sessionField+`:.*$`).FindAllString(text, -1)
	if len(sessionLines) > 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", sessionField,
		)
	}
	rendered := "- " + sessionField + ": " + session
	if len(sessionLines) > 0 {
		text = replaceLiteral(regexp.MustCompile(`(?m)^- `+sessionField+`:.*$`), text, rendered)
	} else {
		text = replaceKeep1(regexp.MustCompile(`(?m)^(- 负责人: .*)$`), text, "\n"+rendered)
	}
	windowLines := regexp.MustCompile(`(?m)^- `+windowField+`:.*$`).FindAllString(text, -1)
	if len(windowLines) > 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", windowField,
		)
	}
	if len(windowLines) > 0 {
		text = regexp.MustCompile(`(?m)^- `+windowField+`:.*\n?`).ReplaceAllString(text, "")
	}
	renderedWindow := strings.TrimRight("- "+windowField+": "+window, " ")
	return replaceKeep1(regexp.MustCompile(`(?m)^(- `+sessionField+`:.*)$`), text, "\n"+renderedWindow), nil
}

func renderTakeoverMetadata(text, agent, session, window string) (string, error) {
	var err error
	text, err = replaceUniqueField(text, "负责人", agent)
	if err != nil {
		return "", err
	}
	text, err = replaceUniqueField(text, sessionField, session)
	if err != nil {
		return "", err
	}
	windowLines := regexp.MustCompile(`(?m)^- `+windowField+`:.*$`).FindAllString(text, -1)
	if len(windowLines) > 1 {
		return "", launchError(
			"launch.task_document_must_contain_exactly_one_metadata_field", windowField,
		)
	}
	if len(windowLines) > 0 {
		return replaceUniqueField(text, windowField, window)
	}
	return replaceKeep1(regexp.MustCompile(`(?m)^(- `+sessionField+`:.*)$`), text, "\n- "+windowField+": "+strings.TrimRight(window, " ")), nil
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
