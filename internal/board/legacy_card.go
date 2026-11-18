package board

import (
	"regexp"
	"strings"
	"time"
)

// Cards written before commit 85df9578 (2026-09-06) name their metadata fields,
// sections and record markers in Chinese. Kander no longer knows those names as
// schema: a card is converted to the English names in memory when it is read, and
// the English names reach the disk with the next controlled write that changes
// the text. This file is the only place that spells the Chinese names.

const legacyCreatedAtLayout = "2006-01-02 15:04"

// legacyCutoff is the first moment a card may no longer be a legacy card. The
// rename commit has an author time of 2026-09-06 23:17 but cards were created
// with the old binary until 23:40, so the cutoff is the next midnight; the
// earliest card with an English header was created at 2026-09-07 01:13.
var legacyCutoff = time.Date(2026, 9, 7, 0, 0, 0, 0, time.Local)

// legacyCreatedRe matches the header line that identifies a legacy card.
var legacyCreatedRe = regexp.MustCompile(`(?m)^- 创建时间:[ \t]*(.*?)[ \t\r]*$`)

var legacyFieldNames = map[string]string{
	"类型":   FieldType,
	"任务组":  FieldTaskGroup,
	"创建时间": FieldCreatedAt,
	"负责人":  FieldOwner,
	"会话":   FieldSession,
	"窗口":   FieldWindow,
	"开始时间": FieldStartedAt,
	"完成时间": FieldFinishedAt,
	"任务分支": FieldTaskBranch,
	"结果":   FieldResult,
}

var legacySectionNames = map[string]string{
	"任务目标":   SectionGoal,
	"用户决策":   SectionUserDecisions,
	"预期成果":   SectionExpectedOutcome,
	"验收条件":   SectionAcceptanceCriteria,
	"威胁模型":   SectionThreatModel,
	"不在本轮范围": SectionOutOfScope,
	"讨论与决策":  SectionDiscussion,
	"实施与验证":  SectionImplementation,
	"完成总结":   SectionSummary,
}

var legacyMarkerNames = map[string]string{
	"自审":   MarkerSelfReview,
	"卡审":   MarkerCardReview,
	"前置任务": MarkerPrerequisites,
}

const legacyPlaceholder = "<填写>"

var (
	legacyFieldRe   = regexp.MustCompile(`(?m)^- (` + alternation(legacyFieldNames) + `):`)
	legacySectionRe = regexp.MustCompile(`(?m)^## (` + alternation(legacySectionNames) + `)([ \t\r]*)$`)
	legacyMarkerRe  = regexp.MustCompile(`(?m)^([ \t]*(?:- )?)(` + alternation(legacyMarkerNames) + `)([ \t]*:)`)
	legacyGroupRe   = regexp.MustCompile(`(?m)^任务组(:)`)
)

func alternation(names map[string]string) string {
	quoted := make([]string, 0, len(names))
	for name := range names {
		quoted = append(quoted, regexp.QuoteMeta(name))
	}
	return strings.Join(quoted, "|")
}

// isLegacyCard reports whether text is a card written before the rename: its
// header holds `- 创建时间:` with a value that parses and lies before the cutoff.
// A missing, unparsable or later value, and a Chinese line in the body, make the
// card an ordinary one.
func isLegacyCard(text string) bool {
	match := legacyCreatedRe.FindStringSubmatch(CardHeader(text))
	if match == nil {
		return false
	}
	created, err := time.ParseInLocation(legacyCreatedAtLayout, match[1], time.Local)
	return err == nil && created.Before(legacyCutoff)
}

// normalizeLegacyCard returns text with the Chinese schema names of a legacy card
// replaced by the English ones, and whether it did so. Any other card comes back
// unchanged. Only schema names change: every other byte of the card is kept.
func normalizeLegacyCard(text string) (string, bool) {
	if !isLegacyCard(text) {
		return text, false
	}
	return renameLegacyNames(text), true
}

// normalizeLegacyInput prepares a submitted body for comparison with the stored
// card, which was read through normalizeLegacyCard. A submission is converted when
// the stored card is legacy, or when the submission is itself a legacy card.
func normalizeLegacyInput(text string, storedLegacy bool) string {
	if storedLegacy {
		return renameLegacyNames(text)
	}
	out, _ := normalizeLegacyCard(text)
	return out
}

// NormalizeLegacyCard is normalizeLegacyCard for readers outside the package that
// read a card document themselves.
func NormalizeLegacyCard(text string) string {
	out, _ := normalizeLegacyCard(text)
	return out
}

func renameLegacyNames(text string) string {
	text = editHeader(text, func(header string) string {
		return legacyFieldRe.ReplaceAllStringFunc(header, func(match string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(match, "- "), ":")
			return "- " + legacyFieldNames[name] + ":"
		})
	})
	text = legacySectionRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := legacySectionRe.FindStringSubmatch(match)
		return "## " + legacySectionNames[parts[1]] + parts[2]
	})
	text = renameInSection(text, SectionDiscussion, func(body string) string {
		body = legacyMarkerRe.ReplaceAllStringFunc(body, func(match string) string {
			parts := legacyMarkerRe.FindStringSubmatch(match)
			return parts[1] + legacyMarkerNames[parts[2]] + parts[3]
		})
		return legacyGroupRe.ReplaceAllString(body, FieldTaskGroup+"$1")
	})
	return strings.ReplaceAll(text, legacyPlaceholder, Placeholder)
}

// renameInSection applies edit to the lines of one `## ` section, heading
// excluded, and leaves the rest of the card as it is.
func renameInSection(text, heading string, edit func(body string) string) string {
	re := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(heading) + `[ \t\r]*$`)
	loc := re.FindStringIndex(text)
	if loc == nil {
		return text
	}
	end := len(text)
	if next := headingRe.FindStringIndex(text[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	return text[:loc[1]] + edit(text[loc[1]:end]) + text[end:]
}
