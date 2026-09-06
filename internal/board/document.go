package board

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/fs"
)

var (
	headingRe    = regexp.MustCompile(`(?m)^## `)
	resultRe     = regexp.MustCompile(`(?m)^- 结果:\s*(.*?)\s*$`)
	oldGroupRe   = regexp.MustCompile(`(?m)^任务组:[ \t]*(.*?)[ \t\r]*$`)
	completeRe   = regexp.MustCompile(`(?m)^- 完成时间:.*$`)
	startRe      = regexp.MustCompile(`(?m)^- 开始时间:.*$`)
	prereqLineRe = regexp.MustCompile(`^前置任务[ \t]*:[ \t]*(.*?)[ \t\r]*$`)
	selfReviewRe = regexp.MustCompile(`(?m)^(?:- )?自审[ \t]*:[ \t]*\S`)
	cardReviewRe = regexp.MustCompile(`(?m)^(?:- )?卡审[ \t]*:[ \t]*\S`)
	checkboxRe   = regexp.MustCompile(`(?m)^- \[[ xX]\][ \t]*\S`)
)

// MetadataFrom reads a `- field:` metadata value.
func MetadataFrom(text, name string) string {
	re := regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(name) + `:[ \t]*(.*?)[ \t\r]*$`)
	match := re.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return match[1]
}

// ReadDocument reads the task document as UTF-8.
func ReadDocument(entry Entry) (string, error) {
	data, err := fs.ReadRegularFile(boardRootFromEntry(entry), entry.Document)
	if err != nil {
		return "", kanbanError(
			"board.task_path_must_not_contain_a_symlink_reparse_point", err.Error(),
		)
	}
	if !utf8.Valid(data) {
		return "", kanbanError("board.task_document_is_not_valid_utf_8", entry.Document)
	}
	return string(data), nil
}

func writeDocument(entry Entry, text string) error {
	err := fs.WriteTextAtomic(boardRootFromEntry(entry), entry.Document, text, true)
	if err != nil {
		return kanbanError(
			"board.task_path_must_not_contain_a_symlink_reparse_point", err.Error(),
		)
	}
	return nil
}

// TitleFrom returns the first level-one heading.
func TitleFrom(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return untitled()
}

// SectionBody returns the body of a ## section.
func SectionBody(text, heading string) (string, bool) {
	re := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(heading) + `\s*$`)
	loc := re.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	rest := text[loc[1]:]
	next := headingRe.FindStringIndex(rest)
	end := len(text)
	if next != nil {
		end = loc[1] + next[0]
	}
	return strings.TrimSpace(text[loc[1]:end]), true
}

func resultFrom(text string) string {
	match := resultRe.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return match[1]
}

// TaskGroupFrom also recognizes legacy group metadata in the discussion section.
func TaskGroupFrom(text string) string {
	if value := MetadataFrom(text, "任务组"); value != "" {
		return value
	}
	discussion, ok := SectionBody(text, "讨论与决策")
	if !ok {
		return ""
	}
	match := oldGroupRe.FindStringSubmatch(discussion)
	if match == nil {
		return ""
	}
	return match[1]
}

func taskGroupFrom(text string) string { return TaskGroupFrom(text) }

func prerequisiteIDsFrom(text, taskID string) ([]string, error) {
	discussion, ok := SectionBody(text, "讨论与决策")
	if !ok {
		return nil, nil
	}
	lines := strings.Split(discussion, "\n")
	var positions []int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "前置任务") {
			positions = append(positions, i)
		}
	}
	if len(positions) == 0 {
		return nil, nil
	}
	first := strings.TrimSpace(lines[0])
	if (first != "```" && first != "```text") || len(positions) != 1 || positions[0] != 1 {
		return nil, kanbanError(
			"board.task_has_invalid_prerequisite_syntax_it_must_be_in", taskID,
		)
	}
	if len(lines) < 3 || strings.TrimSpace(lines[2]) != "```" {
		return nil, kanbanError(
			"board.task_has_invalid_prerequisite_syntax_close_the_code_block", taskID,
		)
	}
	match := prereqLineRe.FindStringSubmatch(strings.TrimSpace(lines[1]))
	if match == nil || match[1] == "" {
		return nil, kanbanError(
			"board.task_has_invalid_prerequisite_syntax_use_n_a_for", taskID,
		)
	}
	value := match[1]
	if value == "N/A" {
		return nil, nil
	}
	raw := strings.Split(value, ",")
	values := make([]string, len(raw))
	for i, part := range raw {
		values[i] = strings.TrimSpace(part)
	}
	for _, part := range values {
		if part == "" || !(taskIDRe.MatchString(part) || taskGroupRe.MatchString(part)) {
			return nil, kanbanError(
				"board.task_has_invalid_prerequisite_syntax", taskID, value,
			)
		}
	}
	return values, nil
}

func completionMetadata(text string) (string, error) {
	value := nowStamp()
	matches := completeRe.FindAllStringIndex(text, -1)
	if len(matches) == 1 {
		return completeRe.ReplaceAllString(text, "- 完成时间: "+value), nil
	}
	if len(matches) > 1 {
		return "", kanbanError("board.task_document_must_contain_exactly_one_completion_time_metadata")
	}
	started := startRe.FindAllStringIndex(text, -1)
	if len(started) != 1 {
		return "", kanbanError("board.task_document_must_contain_exactly_one_start_time_metadata")
	}
	replaced := false
	return startRe.ReplaceAllStringFunc(text, func(s string) string {
		if replaced {
			return s
		}
		replaced = true
		return s + "\n- 完成时间: " + value
	}), nil
}

func incompleteReadySections(text string) []string {
	var missing []string
	for _, heading := range readySections {
		body, ok := SectionBody(text, heading)
		if !ok || body == "" || strings.Contains(body, "<填写>") {
			missing = append(missing, heading)
		}
	}
	return missing
}

func lacksAcceptanceItems(text string) bool {
	body, ok := SectionBody(text, "验收条件")
	return ok && !checkboxRe.MatchString(body)
}

func validateReady(text string) error {
	if missing := incompleteReadySections(text); len(missing) > 0 {
		return kanbanError("board.task_does_not_meet_todo_requirements", strings.Join(missing, ", "))
	}
	if lacksAcceptanceItems(text) {
		return kanbanError("board.task_requires_acceptance_items")
	}
	return nil
}

// validateReviewRecords checks that the post-creation self-review (plus the independent card review for large cards and task-group member cards) left a
// machine-checkable conclusion line in the card's discussion section. It only proves the step was explicitly acknowledged, never its quality.
func validateReviewRecords(entry Entry, text string) error {
	discussion, ok := SectionBody(text, "讨论与决策")
	if !ok || !selfReviewRe.MatchString(discussion) {
		return kanbanError("board.task_requires_self_review_record")
	}
	if entry.Kind == "large" || taskGroupFrom(text) != "" {
		if !cardReviewRe.MatchString(discussion) {
			return kanbanError("board.task_requires_card_review_record")
		}
	}
	return nil
}

func validateTarget(entry Entry, targetState, text string) error {
	switch targetState {
	case "todo":
		if err := validateReady(text); err != nil {
			return err
		}
		return validateReviewRecords(entry, text)
	case "review":
		if MetadataFrom(text, "任务分支") == "" {
			return kanbanError("board.before_moving_to_review_set")
		}
	case "done":
		if resultFrom(text) != "completed" {
			return kanbanError("board.before_moving_to_done_set_completed")
		}
		if entry.Kind == "small" {
			summary, ok := SectionBody(text, "完成总结")
			if !ok || summary == "" || strings.Contains(summary, "<填写>") {
				return kanbanError("board.complete_the_summary_before_moving_a_small_task_to")
			}
		} else {
			report := filepath.Join(entry.Path, "report.md")
			data, err := fs.ReadRegularFile(boardRootFromEntry(entry), report)
			if err != nil {
				if isNotExist(err) {
					return kanbanError("board.complete_report_md_before_moving_a_large_task_to")
				}
				return kanbanError("board.cannot_read_large_task_report_md", err.Error())
			}
			if !utf8.Valid(data) {
				return kanbanError("board.large_task_report_md_is_not_valid_utf_8", report)
			}
			if strings.TrimSpace(string(data)) == "" {
				return kanbanError("board.complete_report_md_before_moving_a_large_task_to")
			}
		}
	case "archived":
		result := resultFrom(text)
		if _, ok := archiveResults[result]; !ok {
			allowed := "cancelled, completed, duplicate, wontfix"
			return kanbanError("board.before_moving_to_archived_set_a_valid_result", allowed)
		}
	case "trash":
		if resultFrom(text) != "trashed" {
			return kanbanError("board.before_moving_to_trash_set_trashed")
		}
	}
	return nil
}

func renderContract(title, taskType string) string {
	created := nowStamp()
	return "# " + title + `

- 类型: ` + typeNames[taskType] + `
- 任务组:
- 创建时间: ` + created + `
- 负责人:
- 会话:
- 窗口:
- 开始时间:
- 完成时间:
- 任务分支:
- 结果:

## 任务目标

<填写>

## 用户决策

N/A

## 预期成果

<填写>

## 验收条件

- [ ] <填写>

## 威胁模型

N/A

## 不在本轮范围

- <填写>

## 讨论与决策

`
}

func smallTaskExtra() string {
	return `## 实施与验证

<填写>

## 完成总结

<填写>
`
}
