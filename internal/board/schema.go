package board

import (
	"regexp"
	"strings"
	"sync"
)

// The card schema tokens. Every language writes the canonical English name; the
// legacy Chinese name of each token is still accepted on read, so cards written
// before the rename keep working without being rewritten.
const (
	FieldType       = "TYPE"
	FieldTaskGroup  = "TASK_GROUP"
	FieldCreatedAt  = "CREATED_AT"
	FieldOwner      = "OWNER"
	FieldSession    = "SESSION"
	FieldWindow     = "WINDOW"
	FieldStartedAt  = "STARTED_AT"
	FieldFinishedAt = "FINISHED_AT"
	FieldTaskBranch = "TASK_BRANCH"
	FieldResult     = "RESULT"
)

const (
	SectionGoal               = "GOAL"
	SectionUserDecisions      = "USER_DECISIONS"
	SectionExpectedOutcome    = "EXPECTED_OUTCOME"
	SectionAcceptanceCriteria = "ACCEPTANCE_CRITERIA"
	SectionThreatModel        = "THREAT_MODEL"
	SectionOutOfScope         = "OUT_OF_SCOPE"
	SectionDiscussion         = "DISCUSSION"
	SectionImplementation     = "IMPLEMENTATION"
	SectionSummary            = "SUMMARY"
)

const (
	MarkerSelfReview    = "SELF_REVIEW"
	MarkerCardReview    = "CARD_REVIEW"
	MarkerPrerequisites = "PREREQUISITES"
	Placeholder         = "<FILL_IN>"
)

// legacyPlaceholder is the pre-rename placeholder. Both spellings mark a section
// as unfinished, so an old card cannot pass the todo gate just by being old.
const legacyPlaceholder = "<填写>"

// legacyToken maps each canonical token to the Chinese name it replaced. A token
// missing from this map has no legacy spelling and is matched by its name alone.
var legacyToken = map[string]string{
	FieldType:       "类型",
	FieldTaskGroup:  "任务组",
	FieldCreatedAt:  "创建时间",
	FieldOwner:      "负责人",
	FieldSession:    "会话",
	FieldWindow:     "窗口",
	FieldStartedAt:  "开始时间",
	FieldFinishedAt: "完成时间",
	FieldTaskBranch: "任务分支",
	FieldResult:     "结果",

	SectionGoal:               "任务目标",
	SectionUserDecisions:      "用户决策",
	SectionExpectedOutcome:    "预期成果",
	SectionAcceptanceCriteria: "验收条件",
	SectionThreatModel:        "威胁模型",
	SectionOutOfScope:         "不在本轮范围",
	SectionDiscussion:         "讨论与决策",
	SectionImplementation:     "实施与验证",
	SectionSummary:            "完成总结",

	MarkerSelfReview:    "自审",
	MarkerCardReview:    "卡审",
	MarkerPrerequisites: "前置任务",
}

// AcceptedNames returns every spelling of a token, canonical name first. An
// unknown name is returned as is, so callers may pass a token this file does not
// own without silently matching nothing.
func AcceptedNames(name string) []string {
	if legacy, ok := legacyToken[name]; ok {
		return []string{name, legacy}
	}
	return []string{name}
}

// TokenPattern is the alternation matching every accepted spelling of a token.
func TokenPattern(name string) string {
	names := AcceptedNames(name)
	quoted := make([]string, len(names))
	for i, value := range names {
		quoted[i] = regexp.QuoteMeta(value)
	}
	return "(?:" + strings.Join(quoted, "|") + ")"
}

var fieldLineCache sync.Map

// FieldLineRe matches the whole `- NAME: value` line of a metadata field in any
// accepted spelling. Other packages reuse it so read and write agree on what
// counts as that field.
func FieldLineRe(name string) *regexp.Regexp {
	if cached, ok := fieldLineCache.Load(name); ok {
		return cached.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`(?m)^- ` + TokenPattern(name) + `:.*$`)
	fieldLineCache.Store(name, re)
	return re
}

// RenderField renders one metadata line. Writers always emit the canonical name,
// so updating a legacy field converts that one line to English.
func RenderField(name, value string) string {
	line := "- " + name + ": " + value
	return strings.TrimRight(line, " ")
}

// ContainsPlaceholder reports whether a section body still holds an unfilled
// placeholder in either spelling.
func ContainsPlaceholder(body string) bool {
	return strings.Contains(body, Placeholder) || strings.Contains(body, legacyPlaceholder)
}

// HasMarkerPrefix reports whether a line starts with a record marker such as
// PREREQUISITES, in any accepted spelling.
func HasMarkerPrefix(line, marker string) bool {
	trimmed := strings.TrimSpace(line)
	for _, name := range AcceptedNames(marker) {
		if strings.HasPrefix(trimmed, name) {
			return true
		}
	}
	return false
}
