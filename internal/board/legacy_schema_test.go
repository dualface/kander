package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyCard is a complete card in the pre-rename Chinese schema, as cards
// created before the token rename still look on disk.
const legacyCard = `# 旧卡

- 类型: Chore
- 任务组:
- 创建时间: 2026-09-01 03:00
- 负责人:
- 会话:
- 窗口:
- 开始时间: 2026-09-01 04:00
- 完成时间:
- 任务分支: legacy-branch
- 结果:

## 任务目标

旧卡目标

## 用户决策

N/A

## 预期成果

旧卡成果

## 验收条件

- [ ] 旧卡条件

## 威胁模型

N/A

## 不在本轮范围

- 无

## 讨论与决策

自审: 通过

## 实施与验证

已完成

## 完成总结

旧卡总结
`

// mixedCard keeps the legacy spelling for the fields a running agent writes back
// and uses the canonical names everywhere else, which is what an old card looks
// like after one command has updated it.
func mixedCard() string {
	text := legacyCard
	for _, pair := range [][2]string{
		{"## 任务目标", "## " + SectionGoal},
		{"## 预期成果", "## " + SectionExpectedOutcome},
		{"## 验收条件", "## " + SectionAcceptanceCriteria},
		{"## 不在本轮范围", "## " + SectionOutOfScope},
		{"## 完成总结", "## " + SectionSummary},
		{"- 任务分支:", "- " + FieldTaskBranch + ":"},
		{"- 结果:", "- " + FieldResult + ":"},
	} {
		text = strings.Replace(text, pair[0], pair[1], 1)
	}
	return text
}

func writeCard(t *testing.T, root, state, taskID, text string) string {
	t.Helper()
	path := filepath.Join(root, state, taskID+".md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A new card carries only canonical tokens, whatever the output language is.
func TestNewCardUsesCanonicalTokensOnly(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	capture(t, func() int { return RunNew([]string{"chore", "schema-tokens", "字段"}) })
	data, err := os.ReadFile(filepath.Join(root, "backlog", todayID("schema-tokens")+".md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	for _, name := range []string{
		FieldType, FieldTaskGroup, FieldCreatedAt, FieldOwner, FieldSession, FieldWindow,
		FieldStartedAt, FieldFinishedAt, FieldTaskBranch, FieldResult,
	} {
		if count := strings.Count(text, "\n- "+name+":"); count != 1 {
			t.Fatalf("field %s appears %d times", name, count)
		}
	}
	for _, name := range []string{
		SectionGoal, SectionUserDecisions, SectionExpectedOutcome, SectionAcceptanceCriteria,
		SectionThreatModel, SectionOutOfScope, SectionDiscussion, SectionImplementation, SectionSummary,
	} {
		if count := strings.Count(text, "\n## "+name+"\n"); count != 1 {
			t.Fatalf("section %s appears %d times", name, count)
		}
	}
	if !strings.Contains(text, Placeholder) {
		t.Fatal("template lost its placeholder")
	}
	for _, legacy := range legacyToken {
		if strings.Contains(text, legacy) {
			t.Fatalf("new card still carries the legacy token %s", legacy)
		}
	}
}

// Reading accepts either spelling, so a legacy or mixed card keeps passing the
// gates it passed before the rename.
func TestGatesAcceptLegacyAndMixedCards(t *testing.T) {
	resetLang(t)
	for _, form := range []struct {
		name string
		text string
	}{
		{"legacy", legacyCard},
		{"mixed", mixedCard()},
	} {
		t.Run(form.name, func(t *testing.T) {
			root := tempBoard(t)
			taskID := todayID(form.name + "-card")
			path := writeCard(t, root, "working", taskID, form.text)

			if code, _, _, err := CheckBoard(root, []string{taskID}, false); err != nil || code != 0 {
				t.Fatalf("check code=%d err=%v", code, err)
			}
			if got := MetadataFrom(form.text, FieldTaskBranch); got != "legacy-branch" {
				t.Fatalf("task branch %q", got)
			}
			if got := MetadataFrom(form.text, FieldStartedAt); got != "2026-09-01 04:00" {
				t.Fatalf("started at %q", got)
			}
			if _, ok := SectionBody(form.text, SectionDiscussion); !ok {
				t.Fatal("discussion section not found")
			}

			// The done gate reads RESULT and SUMMARY through the same aliases.
			if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "done"}) }); code == 0 {
				t.Fatalf("done must require a result: %s", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			updated := strings.Replace(string(data), "- 结果:", "- 结果: completed", 1)
			updated = strings.Replace(updated, "- "+FieldResult+":", "- "+FieldResult+": completed", 1)
			writeCard(t, root, "working", taskID, updated)
			if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "done"}) }); code != 0 {
				t.Fatalf("done: %s", err)
			}
			done, err := os.ReadFile(filepath.Join(root, "done", taskID+".md"))
			if err != nil {
				t.Fatal(err)
			}
			// The completion time is written in place, so the card gains exactly
			// one finished-at field in the canonical spelling.
			if count := len(FieldLineRe(FieldFinishedAt).FindAllString(string(done), -1)); count != 1 {
				t.Fatalf("finished-at fields: %d", count)
			}
			if !strings.Contains(string(done), "- "+FieldFinishedAt+": ") {
				t.Fatalf("completion time not written in canonical form: %s", done)
			}
		})
	}
}

// A legacy card that still writes its group inside the discussion block is read
// through both the legacy and the canonical marker.
func TestLegacyDiscussionMetadataStillParses(t *testing.T) {
	resetLang(t)
	for marker, group := range map[string]string{
		"任务组":          "20260820-legacy-group",
		FieldTaskGroup: "20260820-canonical-group",
	} {
		text := strings.Replace(
			legacyCard, "自审: 通过", marker+": "+group+"\n自审: 通过", 1,
		)
		if got := TaskGroupFrom(text); got != group {
			t.Fatalf("marker %s: group %q", marker, got)
		}
	}
}
