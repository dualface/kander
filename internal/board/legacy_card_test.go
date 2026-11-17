package board

import (
	"os"
	"strings"
	"testing"
)

// legacyDirCard has the structure of a card created before the schema rename:
// Chinese header names (SIZE has none), Chinese section titles, a Chinese review
// marker and a group line in the discussion section.
const legacyDirCard = `# check binding

- 类型: Bug
- SIZE: small
- 任务组:
- 创建时间: 2026-09-06 23:30
- 负责人: cursor
- 会话: cursor 5eba8c16
- 窗口: herdr:wX:tG:wX:p0
- 开始时间: 2026-09-06 23:28
- 完成时间: 2026-09-06 23:45
- 任务分支: check-binding
- 结果: completed

## 任务目标

目标正文

## 用户决策

N/A

## 预期成果

成果正文

## 验收条件

- [x] 条件一

## 威胁模型

N/A

## 不在本轮范围

- 无

## 讨论与决策

任务组: 20260906-legacy-group

自审: 通过

卡审: 通过

## 实施与验证

- 前置任务: 这一行在实施章节, 不是标记
- <填写> 保留给占位符检查

## 完成总结

总结正文
`

// writeLegacyDirCard creates a directory card and replaces its body with the
// given bytes, the way a card written by an old binary sits on disk.
func writeLegacyDirCard(t *testing.T, root, slug, text string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	if err := os.WriteFile(s.Entry.Document, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}

func TestNormalizeLegacyCardRenamesOnlySchemaNames(t *testing.T) {
	got, legacy := normalizeLegacyCard(legacyDirCard)
	if !legacy {
		t.Fatal("legacy card not recognized")
	}
	want := legacyDirCard
	for _, pair := range [][2]string{
		{"- 类型:", "- TYPE:"}, {"- 任务组:", "- TASK_GROUP:"}, {"- 创建时间:", "- CREATED_AT:"},
		{"- 负责人:", "- OWNER:"}, {"- 会话:", "- SESSION:"}, {"- 窗口:", "- WINDOW:"},
		{"- 开始时间:", "- STARTED_AT:"}, {"- 完成时间:", "- FINISHED_AT:"},
		{"- 任务分支:", "- TASK_BRANCH:"}, {"- 结果:", "- RESULT:"},
		{"## 任务目标", "## GOAL"}, {"## 用户决策", "## USER_DECISIONS"},
		{"## 预期成果", "## EXPECTED_OUTCOME"}, {"## 验收条件", "## ACCEPTANCE_CRITERIA"},
		{"## 威胁模型", "## THREAT_MODEL"}, {"## 不在本轮范围", "## OUT_OF_SCOPE"},
		{"## 讨论与决策", "## DISCUSSION"}, {"## 实施与验证", "## IMPLEMENTATION"},
		{"## 完成总结", "## SUMMARY"},
		{"\n任务组: 20260906", "\nTASK_GROUP: 20260906"}, {"\n自审: 通过", "\nSELF_REVIEW: 通过"}, {"\n卡审: 通过", "\nCARD_REVIEW: 通过"},
		{"<填写>", "<FILL_IN>"},
	} {
		if !strings.Contains(want, pair[0]) {
			t.Fatalf("fixture lacks %q", pair[0])
		}
		want = strings.Replace(want, pair[0], pair[1], 1)
	}
	if got != want {
		t.Fatalf("normalized card differs:\n%s\nwant:\n%s", got, want)
	}
	// A marker outside the discussion section stays text.
	if !strings.Contains(got, "- 前置任务: 这一行在实施章节") {
		t.Fatal("marker outside DISCUSSION was renamed")
	}
	again, legacyAgain := normalizeLegacyCard(got)
	if legacyAgain || again != got {
		t.Fatal("normalization is not idempotent")
	}
}

func TestLegacyCardIsJudgedByCreationTimeInTheHeader(t *testing.T) {
	for _, tc := range []struct {
		created string
		legacy  bool
	}{
		{"2026-09-06 23:59", true},
		{"2026-09-07 00:00", false},
		{"2026-10-01 10:00", false},
		{"not a time", false},
	} {
		text := strings.Replace(legacyDirCard, "2026-09-06 23:30", tc.created, 1)
		got, legacy := normalizeLegacyCard(text)
		if legacy != tc.legacy || (!legacy && got != text) {
			t.Fatalf("created %q: legacy=%v", tc.created, legacy)
		}
	}
	// Chinese header names without a creation time are not a legacy card.
	noTime := strings.Replace(legacyDirCard, "- 创建时间: 2026-09-06 23:30\n", "", 1)
	if got, legacy := normalizeLegacyCard(noTime); legacy || got != noTime {
		t.Fatal("card without creation time converted")
	}
	// A body line never makes a card legacy.
	body := "# t\n\n- TYPE: Bug\n- CREATED_AT: 2026-10-01 10:00\n\n## DISCUSSION\n\n- 创建时间: 2026-01-01 00:00\n"
	if got, legacy := normalizeLegacyCard(body); legacy || got != body {
		t.Fatal("body line made the card legacy")
	}
}

func TestLegacyCardIsReadWithEnglishNamesAndDiskStaysUntouched(t *testing.T) {
	root := tempBoard(t)
	before := writeLegacyDirCard(t, root, "legacy-read", legacyDirCard)
	s := transactionSnapshot(t, root, before.Entry.TaskID)
	if strings.Contains(s.Text, "创建时间") || !strings.Contains(s.Text, "- CREATED_AT: 2026-09-06 23:30") {
		t.Fatalf("snapshot not normalized:\n%s", s.Text)
	}
	if got := MetadataFrom(s.Text, FieldResult); got != "completed" {
		t.Fatalf("result %q", got)
	}
	if got := TaskGroupFrom(s.Text); got != "20260906-legacy-group" {
		t.Fatalf("group %q", got)
	}
	if err := validateReviewRecords(s.Entry, s.Text); err != nil {
		t.Fatalf("self-review gate: %v", err)
	}
	if _, ok := SectionBody(s.Text, SectionGoal); !ok {
		t.Fatal("goal section not found")
	}
	if code, _, _, err := CheckBoard(root, []string{s.Entry.TaskID}, false); err != nil || code != 0 {
		t.Fatalf("check code=%d err=%v", code, err)
	}

	// show --document spec.md prints what the snapshot holds.
	_, text, err := ReadDocumentSnapshot(root, s.Entry.TaskID, "spec.md")
	if err != nil || text != s.Text {
		t.Fatalf("document text differs from snapshot: %v", err)
	}

	// Reading never writes: the bytes and the revision stay.
	disk, err := os.ReadFile(s.Entry.Document)
	if err != nil || string(disk) != legacyDirCard {
		t.Fatalf("disk changed by reading: %v", err)
	}
	if after := transactionSnapshot(t, root, s.Entry.TaskID); after.Revision != s.Revision {
		t.Fatalf("revision %d -> %d", s.Revision, after.Revision)
	}
	raw, err := readDocument(s.Entry)
	if err != nil || raw != legacyDirCard {
		t.Fatalf("readDocument must return the disk bytes: %v", err)
	}
}

func TestLegacyCardTextWriteStoresEnglishNames(t *testing.T) {
	root := tempBoard(t)
	s := writeLegacyDirCard(t, root, "legacy-write", strings.Replace(legacyDirCard, "- 结果: completed", "- 结果:", 1))
	edited := strings.Replace(s.Text, "TASK_GROUP: 20260906-legacy-group", "TASK_GROUP: 20260906-legacy-group\n\n新增讨论一行", 1)
	if edited == s.Text {
		t.Fatal("fixture did not change the card")
	}
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: edited, ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(s.Entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != edited {
		t.Fatalf("disk text differs from the update:\n%s", disk)
	}
	for _, names := range []map[string]string{legacyFieldNames, legacySectionNames} {
		for name := range names {
			if strings.Contains(string(disk), "- "+name+":") || strings.Contains(string(disk), "## "+name+"\n") {
				t.Fatalf("legacy name %s still on disk", name)
			}
		}
	}
	for _, marker := range []string{"\n自审:", "\n卡审:", "\n任务组:"} {
		if strings.Contains(string(disk), marker) {
			t.Fatalf("legacy marker %q still on disk", marker)
		}
	}
	if strings.Contains(string(disk), legacyPlaceholder) {
		t.Fatal("legacy placeholder still on disk")
	}

	// A submission that still spells the legacy names is read the same way.
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	legacySubmission := strings.Replace(legacyDirCard, "- 结果: completed", "- 结果:", 1)
	if err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: strings.Replace(legacySubmission, "自审: 通过", "自审: 通过\n再一行", 1), ExpectedRevision: s.Revision}); err != nil {
		t.Fatalf("legacy submission: %v", err)
	}
}

func TestLegacyCardMoveWithoutTextChangeLeavesDiskAlone(t *testing.T) {
	root := tempBoard(t)
	s := writeLegacyDirCard(t, root, "legacy-move", legacyDirCard)
	moved, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(moved.Document)
	if err != nil || string(disk) != legacyDirCard {
		t.Fatalf("a move that changes no text rewrote the card: %v", err)
	}
}

const bodyLines = "- 结果: 测试通过\n- RESULT: 测试通过\n- 负责人: 某人\n- OWNER: x\n- SESSION: x\n- WINDOW: x\n- STARTED_AT: x\n"

func TestBodyMetadataLinesAreOrdinaryText(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "body-lines", false)
	text := readyText(s)
	text = strings.Replace(text, "## DISCUSSION\n\n", "## DISCUSSION\n\n"+bodyLines, 1)
	text = strings.Replace(text, "## GOAL\n\n已确定的任务范围", "## GOAL\n\n已确定的任务范围 <填写>", 1)
	text += "\n## 任务目标\n\n自审: 通过\n"
	if text == readyText(s) {
		t.Fatal("fixture did not change the card")
	}
	// Accepted in backlog.
	updateSnapshot(t, root, s, text)
	s = transactionSnapshot(t, root, s.Entry.TaskID)

	if got := MetadataFrom(s.Text, FieldResult); got != "" {
		t.Fatalf("result %q", got)
	}
	if got := TaskSummaryOf(s.Entry, s.Text).Result; got != "" {
		t.Fatalf("summary result %q", got)
	}
	if body, _ := SectionBody(s.Text, SectionGoal); body != "已确定的任务范围 <填写>" {
		t.Fatalf("goal body %q", body)
	}
	if missing := incompleteReadySections(s.Text); len(missing) != 0 {
		t.Fatalf("legacy placeholder blocked the todo gate: %v", missing)
	}

	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	working, err := MoveWithOptions(todo, root, "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	if got := MetadataFrom(s.Text, FieldOwner); got != "codex" {
		t.Fatalf("owner %q", got)
	}
	if !strings.Contains(s.Text, bodyLines) {
		t.Fatalf("body lines changed:\n%s", s.Text)
	}
	// Accepted in working as well.
	updateSnapshot(t, root, s, strings.Replace(s.Text, bodyLines, bodyLines+"- 结果: 再一行\n", 1))
}

func TestSelfReviewGateIgnoresChineseMarkerOnNewCard(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "marker-gate", false)
	text := readyText(s)
	text = strings.Replace(text, "SELF_REVIEW: 通过\nCARD_REVIEW: 通过", "自审: 通过\n卡审: 通过", 1)
	if err := validateReviewRecords(Entry{Kind: "small"}, text); err == nil {
		t.Fatal("Chinese marker satisfied the self-review gate")
	}
}

func TestMetadataWritesTouchOnlyTheHeader(t *testing.T) {
	text := "# t\n\n- TYPE: Bug\n- SIZE: small\n- OWNER:\n- SESSION:\n- WINDOW: old\n- STARTED_AT:\n- FINISHED_AT:\n- RESULT:\n\n## DISCUSSION\n\n" + bodyLines + "- FINISHED_AT: x\n"
	check := func(name, got string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasSuffix(got, "\n## DISCUSSION\n\n"+bodyLines+"- FINISHED_AT: x\n") && !strings.Contains(got, "## DISCUSSION\n\n"+bodyLines+"- FINISHED_AT: x\n") {
			t.Fatalf("%s changed the body:\n%s", name, got)
		}
	}
	started, err := renderStart(text, "codex", TaskStartInput{Session: "codex s1", Window: "w1"})
	check("renderStart", started, err)
	for _, want := range []string{"- OWNER: codex\n", "- SESSION: codex s1\n", "- WINDOW: w1\n"} {
		if !strings.Contains(CardHeader(started), want) {
			t.Fatalf("header lacks %q:\n%s", want, started)
		}
	}
	taken, err := renderTakeover(started, TakeoverInput{Owner: "claude", Session: "claude s2", Window: "w2"})
	check("renderTakeover", taken, err)
	if MetadataFrom(taken, FieldOwner) != "claude" || MetadataFrom(taken, FieldWindow) != "w2" {
		t.Fatalf("takeover header:\n%s", taken)
	}
	set, err := setMetadata(text, FieldResult, "completed")
	check("setMetadata", set, err)
	if MetadataFrom(set, FieldResult) != "completed" {
		t.Fatalf("result not set:\n%s", set)
	}
	completed, err := completionMetadata(started)
	check("completionMetadata", completed, err)
	if MetadataFrom(completed, FieldFinishedAt) == "" {
		t.Fatalf("finished-at not set:\n%s", completed)
	}
}

func TestHeaderRepeatedFieldIsStillRejected(t *testing.T) {
	text := "# t\n\n- TYPE: Bug\n- OWNER:\n- OWNER:\n\n## DISCUSSION\n"
	if _, err := setMetadata(text, FieldOwner, "x"); err == nil {
		t.Fatal("duplicate header field accepted")
	}
	if got := len(fieldLines(text+"- OWNER: body\n", FieldOwner)); got != 2 {
		t.Fatalf("header lines %d", got)
	}
}

// Start, WINDOW write-back, takeover and completion write only the header fields
// of a card whose body repeats the same field lines.
func TestExecutionWritesLeaveBodyFieldLinesAlone(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "body-execution", false)
	text := strings.ReplaceAll(readyText(s), "<FILL_IN>", "fixture")
	text = strings.Replace(text, "## DISCUSSION\n\n", "## DISCUSSION\n\n"+bodyLines+"- WINDOW: body-window\n- FINISHED_AT: body\n- TASK_BRANCH: body\n", 1)
	updateSnapshot(t, root, s, text)
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	body := bodyLines + "- WINDOW: body-window\n- FINISHED_AT: body\n- TASK_BRANCH: body\n"
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := BeginTaskStart(todo, root, TaskStartInput{Owner: "codex", Session: "codex s1", Window: "w1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	check := func(step, owner, window string) {
		t.Helper()
		got := transactionSnapshot(t, root, moved.TaskID)
		if !strings.Contains(got.Text, body) {
			t.Fatalf("%s changed the body lines:\n%s", step, got.Text)
		}
		if MetadataFrom(got.Text, FieldOwner) != owner || MetadataFrom(got.Text, FieldWindow) != window {
			t.Fatalf("%s: owner %q window %q", step, MetadataFrom(got.Text, FieldOwner), MetadataFrom(got.Text, FieldWindow))
		}
	}
	check("start", "codex", "w1")
	if err = ConfirmTaskStart(root, moved); err != nil {
		t.Fatal(err)
	}
	cursor := bound(t, root, moved.TaskID)
	if _, err = BeginTakeover(root, cursor.Entry, TakeoverInput{Owner: "claude", Session: "claude s2", Window: "w3"}); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	check("takeover", "claude", "w3")
	cursor = bound(t, root, moved.TaskID)
	if _, err = RecordExecutionWindow(root, cursor.Entry, "w4", ""); err != nil {
		t.Fatalf("window: %v", err)
	}
	check("window write-back", "claude", "w4")
}
