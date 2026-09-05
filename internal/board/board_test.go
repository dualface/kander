package board

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

func resetLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
}

func tempBoard(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, state := range States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(EnvBoardDir, root)
	rules := filepath.Join(t.TempDir(), "KANDER-KANBAN-RULES.md")
	if err := os.WriteFile(rules, []byte("# 全局文件看板规则\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installPathsFn = func() (config.InstallPaths, error) {
		return config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Dir(rules)}, nil
	}
	t.Cleanup(func() { installPathsFn = config.CurrentInstallPaths })
	return root
}

func capture(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	code := fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(outR)
	errb, _ := io.ReadAll(errR)
	_ = outR.Close()
	_ = errR.Close()
	return code, string(out), string(errb)
}

func captureIn(t *testing.T, input string, fn func() int) (int, string, string) {
	t.Helper()
	oldIn := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	go func() {
		_, _ = io.WriteString(w, input)
		_ = w.Close()
	}()
	code, out, errb := capture(t, fn)
	os.Stdin = oldIn
	_ = r.Close()
	return code, out, errb
}

func makeReady(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, replacement := range []string{"实现目标", "产生可验证结果", "满足验收", "无额外范围"} {
		text = strings.Replace(text, "<填写>", replacement, 1)
	}
	text = strings.Replace(text, "## 讨论与决策\n", "## 讨论与决策\n\n自审: 通过\n卡审: 通过\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func complete(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "- 结果:\n", "- 结果: completed\n", 1)
	text = strings.ReplaceAll(text, "<填写>", "验证通过")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setMeta(t *testing.T, path, old, neu string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, neu, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func todayID(slug string) string {
	return time.Now().Format("20060102") + "-" + slug + "-task"
}

func TestSmallAndLargeLifecycle(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	smallID := todayID("small-fix")
	if code, _, err := capture(t, func() int { return RunNew([]string{"bug", "small-fix", "修复小问题"}) }); code != 0 {
		t.Fatalf("new small: %s", err)
	}
	small := filepath.Join(root, "backlog", smallID+".md")
	if code, _, err := capture(t, func() int { return RunMove([]string{smallID, "todo"}) }); code == 0 {
		t.Fatalf("expected todo reject: %s", err)
	}
	makeReady(t, small)
	if code, _, err := capture(t, func() int { return RunMove([]string{smallID, "todo"}) }); code != 0 {
		t.Fatalf("todo: %s", err)
	}
	if code, _, err := capture(t, func() int { return RunMove([]string{smallID, "working"}) }); code != 0 {
		t.Fatalf("working: %s", err)
	}
	small = filepath.Join(root, "working", smallID+".md")
	complete(t, small)
	if code, _, err := capture(t, func() int { return RunMove([]string{smallID, "done"}) }); code != 0 {
		t.Fatalf("done: %s", err)
	}

	largeID := todayID("large-feature")
	if code, _, err := capture(t, func() int { return RunNew([]string{"--large", "feature", "large-feature", "大型功能"}) }); code != 0 {
		t.Fatalf("new large: %s", err)
	}
	spec := filepath.Join(root, "backlog", largeID, "spec.md")
	makeReady(t, spec)
	capture(t, func() int { return RunMove([]string{largeID, "todo"}) })
	capture(t, func() int { return RunMove([]string{largeID, "working"}) })
	spec = filepath.Join(root, "working", largeID, "spec.md")
	complete(t, spec)
	setMeta(t, spec, "- 完成时间:\n", "")
	if code, _, _ := capture(t, func() int { return RunMove([]string{largeID, "done"}) }); code == 0 {
		t.Fatal("expected missing report")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(spec), "report.md"), []byte("# 完成报告\n\n验证通过.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, err := capture(t, func() int { return RunMove([]string{largeID, "done"}) }); code != 0 {
		t.Fatalf("large done: %s", err)
	}

	code, out, _ := capture(t, func() int { return RunList([]string{"done"}) })
	if code != 0 || !strings.Contains(out, smallID) || !strings.Contains(out, largeID) {
		t.Fatalf("list done: %s", out)
	}
	if !regexp.MustCompile(`done\s+small\s+\d{4}-\d{2}-\d{2} \d{2}:\d{2}`).MatchString(out) {
		t.Fatalf("timestamp: %s", out)
	}
	completed, _ := os.ReadFile(filepath.Join(root, "done", smallID+".md"))
	if !regexp.MustCompile(`(?m)^- 完成时间: \d{4}-\d{2}-\d{2} \d{2}:\d{2}$`).Match(completed) {
		t.Fatalf("completion field: %s", completed)
	}
	code, out, _ = capture(t, func() int { return RunCheck(nil) })
	if code != 0 || out != "通过: 0 个任务\n" {
		t.Fatalf("check: code=%d out=%q", code, out)
	}
	code, out, _ = capture(t, func() int { return RunCheck([]string{"--all"}) })
	if code != 0 || out != "通过: 2 个任务\n" {
		t.Fatalf("check --all: code=%d out=%q", code, out)
	}
}

func TestPickAndReviewTransitions(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("pick")
	capture(t, func() int { return RunNew([]string{"chore", "pick", "挑选任务"}) })
	task := filepath.Join(root, "backlog", taskID+".md")
	code, _, err := capture(t, func() int { return RunPick([]string{taskID}) })
	if code == 0 || !strings.Contains(err, "任务未满足 todo 条件") {
		t.Fatalf("pick unread: %s", err)
	}
	makeReady(t, task)
	if code, _, err := capture(t, func() int { return RunPick([]string{taskID}) }); code != 0 {
		t.Fatalf("pick: %s", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "todo", taskID+".md")); statErr != nil {
		t.Fatal(statErr)
	}
	code, _, err = capture(t, func() int { return RunPick([]string{taskID}) })
	if code == 0 || !strings.Contains(err, "不允许迁移: todo -> todo") {
		t.Fatalf("repick: %s", err)
	}
	if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "backlog"}) }); code != 0 {
		t.Fatalf("todo to backlog: %s", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "backlog", taskID+".md")); statErr != nil {
		t.Fatal(statErr)
	}
	if code, _, err := capture(t, func() int { return RunPick([]string{taskID}) }); code != 0 {
		t.Fatalf("repick after backlog: %s", err)
	}

	code, _, err = capture(t, func() int { return RunMove([]string{taskID, "review"}) })
	if code == 0 || !strings.Contains(err, "todo -> review") {
		t.Fatalf("skip review: %s", err)
	}
	capture(t, func() int { return RunMove([]string{taskID, "working"}) })
	working := filepath.Join(root, "working", taskID+".md")
	code, _, err = capture(t, func() int { return RunMove([]string{taskID, "review"}) })
	if code == 0 || !strings.Contains(err, "任务分支") {
		t.Fatalf("review branch: %s", err)
	}
	setMeta(t, working, "- 任务分支:\n", "- 任务分支: review-flow\n")
	if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "review"}) }); code != 0 {
		t.Fatalf("to review: %s", err)
	}
	review := filepath.Join(root, "review", taskID+".md")
	capture(t, func() int { return RunMove([]string{taskID, "working"}) })
	capture(t, func() int { return RunMove([]string{taskID, "review"}) })
	if code, _, _ := capture(t, func() int { return RunMove([]string{taskID, "done"}) }); code == 0 {
		t.Fatal("done without complete")
	}
	complete(t, review)
	if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "done"}) }); code != 0 {
		t.Fatalf("review to done: %s", err)
	}
}

func TestListFormats(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("COLORFGBG", "15;0")
	_ = os.Unsetenv("NO_COLOR")
	taskID := todayID("list-table")
	capture(t, func() int { return RunNew([]string{"chore", "list-table", "表格输出"}) })
	largeID := todayID("list-large")
	capture(t, func() int { return RunNew([]string{"--large", "chore", "list-large", "大型表格输出"}) })
	_, out, _ := capture(t, func() int { return RunList([]string{"backlog"}) })
	plain := regexp.MustCompile(`\033\[[0-9;]*m`).ReplaceAllString(out, "")
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	if lines[0] != "状态     规模   时间  任务 ID / 标题" {
		t.Fatalf("header %q", lines[0])
	}
	if !strings.Contains(plain, "backlog  small  -     "+taskID+"  表格输出") {
		t.Fatalf("small row: %s", plain)
	}
	if !strings.Contains(plain, "backlog  large  -     "+largeID+"  大型表格输出") {
		t.Fatalf("large row: %s", plain)
	}
	if !strings.Contains(out, "\033[90mbacklog") || !strings.Contains(out, "\033[1;95mlarge") {
		t.Fatalf("colors: %q", out)
	}
	_, mobile, _ := capture(t, func() int { return RunList([]string{"--mobile", "backlog"}) })
	if !strings.Contains(mobile, taskID) || strings.Count(mobile, "\n") < 3 {
		t.Fatalf("mobile: %q", mobile)
	}
	_, empty, _ := capture(t, func() int { return RunList([]string{"--mobile", "done"}) })
	if empty != "" {
		t.Fatalf("empty mobile %q", empty)
	}

	legacy := filepath.Join(root, "done", todayID("legacy-done")+".md")
	if err := os.WriteFile(legacy, []byte("# 历史任务\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2024, 1, 2, 3, 4, 0, 0, time.Local)
	_ = os.Chtimes(legacy, mtime, mtime)
	_, listed, _ := capture(t, func() int { return RunList([]string{"done"}) })
	if !strings.Contains(listed, "2024-01-02 03:04") {
		t.Fatalf("mtime list: %s", listed)
	}
}

func TestCheckDependenciesAndScope(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	ids := map[string]string{}
	for _, slug := range []string{"dependency-source", "dependency-internal", "dependency-external", "dependency-group-first", "dependency-group-second"} {
		id := todayID(slug)
		ids[slug] = id
		capture(t, func() int { return RunNew([]string{"chore", slug, "任务 " + slug}) })
		path := filepath.Join(root, "backlog", id+".md")
		makeReady(t, path)
		capture(t, func() int { return RunMove([]string{id, "todo"}) })
	}
	todo := func(slug string) string { return filepath.Join(root, "todo", ids[slug]+".md") }
	setMeta(t, todo("dependency-source"), "- 任务组:\n", "- 任务组: 20260901-source-group\n")
	setMeta(t, todo("dependency-internal"), "- 任务组:\n", "- 任务组: 20260901-source-group\n")
	setMeta(t, todo("dependency-external"), "- 任务组:\n", "- 任务组: 20260901-external-group\n")
	setMeta(t, todo("dependency-group-first"), "- 任务组:\n", "- 任务组: 20260901-expanded-group\n")
	setMeta(t, todo("dependency-group-second"), "- 任务组:\n", "- 任务组: 20260901-expanded-group\n")
	prereq := ids["dependency-internal"] + ", " + ids["dependency-external"] + ", 20260901-expanded-group"
	setMeta(t, todo("dependency-source"), "## 讨论与决策\n\n", "## 讨论与决策\n\n```text\n前置任务: "+prereq+"\n```\n\n")
	setMeta(t, todo("dependency-internal"), "## 讨论与决策\n\n", "## 讨论与决策\n\n```text\n前置任务: N/A\n```\n\n")

	board, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := TaskDependenciesOf(board.Entries[ids["dependency-source"]], board, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deps.InternalTasks, ",") != ids["dependency-internal"] {
		t.Fatalf("internal %+v", deps)
	}
	if strings.Join(deps.ExternalTasks, ",") != ids["dependency-external"] {
		t.Fatalf("external %+v", deps)
	}
	if strings.Join(deps.TaskGroups, ",") != "20260901-expanded-group" {
		t.Fatalf("groups %+v", deps)
	}
	wantExpanded := []string{ids["dependency-internal"], ids["dependency-external"], ids["dependency-group-first"], ids["dependency-group-second"]}
	if strings.Join(deps.ExpandedTaskIDs, ",") != strings.Join(wantExpanded, ",") {
		t.Fatalf("expanded %+v want %v", deps.ExpandedTaskIDs, wantExpanded)
	}
	code, out, _ := capture(t, func() int { return RunCheck(nil) })
	if code != 0 || out != "通过: 5 个任务\n" {
		t.Fatalf("check deps: %d %q", code, out)
	}

	missingID := todayID("dependency-missing")
	capture(t, func() int { return RunNew([]string{"chore", "dependency-missing", "任务 dependency-missing"}) })
	mp := filepath.Join(root, "backlog", missingID+".md")
	makeReady(t, mp)
	capture(t, func() int { return RunMove([]string{missingID, "todo"}) })
	setMeta(t, filepath.Join(root, "todo", missingID+".md"), "- 任务组:\n", "- 任务组: 20260901-validation-group\n")
	setMeta(t, filepath.Join(root, "todo", missingID+".md"), "## 讨论与决策\n\n", "## 讨论与决策\n\n```text\n前置任务: 20260901-does-not-exist-task\n```\n\n")
	code, _, errb := capture(t, func() int { return RunCheck([]string{missingID}) })
	if code == 0 || !strings.Contains(errb, "20260901-does-not-exist-task") {
		t.Fatalf("missing prereq: %s", errb)
	}

	notes := filepath.Join(root, "backlog", "notes.md")
	_ = os.WriteFile(notes, []byte("随手记"), 0o644)
	code, _, errb = capture(t, func() int { return RunCheck(nil) })
	if code == 0 || !strings.Contains(errb, "notes.md") {
		t.Fatalf("invalid entry: %s", errb)
	}
	cleanID := ids["dependency-internal"]
	code, out, errb = capture(t, func() int { return RunCheck([]string{cleanID}) })
	if code != 0 || out != "通过: 1 个任务\n" || errb != "" {
		t.Fatalf("targeted: code=%d out=%q err=%q", code, out, errb)
	}

	_ = os.WriteFile(filepath.Join(root, "done", "notes.md"), []byte("旧笔记"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "archived", "notes.md"), []byte("归档笔记"), 0o644)
	empty := t.TempDir()
	for _, state := range States {
		_ = os.Mkdir(filepath.Join(empty, state), 0o755)
	}
	t.Setenv(EnvBoardDir, empty)
	code, out, errb = capture(t, func() int { return RunCheck(nil) })
	if code != 0 || out != "通过: 0 个任务\n" {
		t.Fatalf("empty check %d %q %q", code, out, errb)
	}
	_ = os.WriteFile(filepath.Join(empty, "done", "notes.md"), []byte("旧笔记"), 0o644)
	_ = os.WriteFile(filepath.Join(empty, "archived", "notes.md"), []byte("归档笔记"), 0o644)
	code, out, errb = capture(t, func() int { return RunCheck(nil) })
	if code != 0 || out != "通过: 0 个任务\n" || errb != "" {
		t.Fatalf("skip done/archived: %d %q %q", code, out, errb)
	}
	code, out, errb = capture(t, func() int { return RunCheck([]string{"--all"}) })
	if code == 0 || !strings.Contains(out, "已检查: 0 个有效, 2 个无效") {
		t.Fatalf("--all: %d %q %q", code, out, errb)
	}
}

func TestInitLayoutAndArchive(t *testing.T) {
	resetLang(t)
	project := t.TempDir()
	t.Setenv(EnvBoardDir, "")
	rulesDir := filepath.Join(t.TempDir(), "rules")
	_ = os.MkdirAll(rulesDir, 0o755)
	_ = os.WriteFile(filepath.Join(rulesDir, "KANDER-KANBAN-RULES.md"), []byte("# 全局文件看板规则\n"), 0o644)
	installPathsFn = func() (config.InstallPaths, error) {
		return config.InstallPaths{Mode: config.ModeGlobal, RulesDir: rulesDir}, nil
	}
	t.Cleanup(func() { installPathsFn = config.CurrentInstallPaths })

	code, out, err := capture(t, func() int { return RunInit([]string{project}) })
	if code != 0 {
		t.Fatalf("init: %s", err)
	}
	if !strings.Contains(out, "已初始化:") || !strings.Contains(out, "规则:") {
		t.Fatalf("init out %q", out)
	}
	for _, state := range States {
		if st, e := os.Stat(filepath.Join(project, "kanban", state)); e != nil || !st.IsDir() {
			t.Fatalf("missing %s: %v", state, e)
		}
	}
	if code, _, err := capture(t, func() int { return RunInit([]string{project}) }); code != 0 {
		t.Fatalf("init idempotent: %s", err)
	}

	gitProj := t.TempDir()
	cmd := exec.Command("git", "init", "-q", gitProj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %s", err, out)
	}
	if code, _, err := capture(t, func() int { return RunInit([]string{gitProj}) }); code != 0 {
		t.Fatalf("git init board: %s", err)
	}
	exclude, _ := os.ReadFile(filepath.Join(gitProj, ".git", "info", "exclude"))
	count := 0
	for _, line := range strings.Split(string(exclude), "\n") {
		if line == "/kanban/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("exclude %q", exclude)
	}
	capture(t, func() int { return RunInit([]string{gitProj}) })
	exclude, _ = os.ReadFile(filepath.Join(gitProj, ".git", "info", "exclude"))
	count = 0
	for _, line := range strings.Split(string(exclude), "\n") {
		if line == "/kanban/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("exclude twice %q", exclude)
	}

	root := tempBoard(t)
	taskID := todayID("retired")
	capture(t, func() int { return RunNew([]string{"research", "retired", "终止研究"}) })
	task := filepath.Join(root, "backlog", taskID+".md")
	if code, _, _ := capture(t, func() int { return RunMove([]string{taskID, "archived"}) }); code == 0 {
		t.Fatal("archive without result")
	}
	setMeta(t, task, "- 结果:\n", "- 结果: cancelled\n")
	if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "archived"}) }); code != 0 {
		t.Fatalf("archive: %s", err)
	}
	task = filepath.Join(root, "archived", taskID+".md")
	if code, _, _ := capture(t, func() int { return RunMove([]string{taskID, "trash"}) }); code == 0 {
		t.Fatal("trash without result")
	}
	setMeta(t, task, "- 结果: cancelled\n", "- 结果: trashed\n")
	if code, _, err := capture(t, func() int { return RunMove([]string{taskID, "trash"}) }); code != 0 {
		t.Fatalf("trash: %s", err)
	}
}

func TestLegacyReviewAutocreateAndReject(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("legacy-board")
	capture(t, func() int { return RunNew([]string{"chore", "legacy-board", "任务 legacy-board"}) })
	makeReady(t, filepath.Join(root, "backlog", taskID+".md"))
	capture(t, func() int { return RunMove([]string{taskID, "todo"}) })
	if err := os.RemoveAll(filepath.Join(root, "review")); err != nil {
		t.Fatal(err)
	}
	if code, out, err := capture(t, func() int { return RunList(nil) }); code != 0 {
		t.Fatalf("list recreate review: %s %s", err, out)
	}
	if st, err := os.Stat(filepath.Join(root, "review")); err != nil || !st.IsDir() {
		t.Fatal("review not recreated")
	}

	if err := os.RemoveAll(filepath.Join(root, "review")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "archived")); err != nil {
		t.Fatal(err)
	}
	code, _, errb := capture(t, func() int { return RunList(nil) })
	if code == 0 || !strings.Contains(errb, "状态目录不存在") {
		t.Fatalf("missing archived: %s", errb)
	}
	if _, err := os.Stat(filepath.Join(root, "review")); !os.IsNotExist(err) {
		t.Fatal("must not create review when other states missing")
	}

	root = tempBoard(t)
	capture(t, func() int { return RunNew([]string{"chore", "occupied-review", "任务 occupied-review"}) })
	makeReady(t, filepath.Join(root, "backlog", todayID("occupied-review")+".md"))
	_ = os.RemoveAll(filepath.Join(root, "review"))
	_ = os.WriteFile(filepath.Join(root, "review"), []byte(""), 0o644)
	code, _, errb = capture(t, func() int { return RunList(nil) })
	if code == 0 || !strings.Contains(errb, "状态路径不是目录") {
		t.Fatalf("review file: %s", errb)
	}
}

func TestDuplicateCycleAndSymlinkTargets(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("duplicate")
	capture(t, func() int { return RunNew([]string{"chore", "duplicate", "重复检测"}) })
	if code, _, _ := capture(t, func() int { return RunMove([]string{taskID, "working"}) }); code == 0 {
		t.Fatal("backlog to working")
	}
	_ = os.Mkdir(filepath.Join(root, "todo", taskID), 0o755)
	_ = os.WriteFile(filepath.Join(root, "todo", taskID, "spec.md"), []byte("# 重复\n"), 0o644)
	code, _, errb := capture(t, func() int { return RunCheck(nil) })
	if code == 0 || !strings.Contains(errb, "重复任务 ID") {
		t.Fatalf("dup: %s", errb)
	}

	root = tempBoard(t)
	first := todayID("dependency-cycle-first")
	second := todayID("dependency-cycle-second")
	for _, slug := range []string{"dependency-cycle-first", "dependency-cycle-second"} {
		capture(t, func() int { return RunNew([]string{"chore", slug, "任务 " + slug}) })
		p := filepath.Join(root, "backlog", todayID(slug)+".md")
		makeReady(t, p)
		capture(t, func() int { return RunMove([]string{todayID(slug), "todo"}) })
	}
	setMeta(t, filepath.Join(root, "todo", first+".md"), "- 任务组:\n", "- 任务组: 20260901-cycle-first-group\n")
	setMeta(t, filepath.Join(root, "todo", second+".md"), "- 任务组:\n", "- 任务组: 20260901-cycle-second-group\n")
	setMeta(t, filepath.Join(root, "todo", first+".md"), "## 讨论与决策\n\n", "## 讨论与决策\n\n```text\n前置任务: "+second+"\n```\n\n")
	setMeta(t, filepath.Join(root, "todo", second+".md"), "## 讨论与决策\n\n", "## 讨论与决策\n\n```text\n前置任务: "+first+"\n```\n\n")
	code, _, errb = capture(t, func() int { return RunCheck([]string{first}) })
	if code == 0 || !strings.Contains(errb, "依赖成环") {
		t.Fatalf("cycle: %s", errb)
	}

	if runtime.GOOS == "windows" {
		return
	}
	root = tempBoard(t)
	linkID := todayID("target-link")
	outside := filepath.Join(t.TempDir(), "outside-target.md")
	_ = os.WriteFile(outside, []byte("outside\n"), 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "todo", linkID+".md")); err != nil {
		t.Fatal(err)
	}
	code, _, errb = capture(t, func() int { return RunCheck([]string{linkID}) })
	if code == 0 || !strings.Contains(errb, "符号链接/重解析点") {
		t.Fatalf("symlink: %s", errb)
	}
	_ = os.Remove(filepath.Join(root, "todo", linkID+".md"))
	_ = os.Mkdir(filepath.Join(root, "todo", linkID), 0o755)
	code, _, errb = capture(t, func() int { return RunCheck([]string{linkID}) })
	if code == 0 || !strings.Contains(errb, "大任务缺少 spec.md") {
		t.Fatalf("missing spec: %s", errb)
	}
}

func TestPickPromptAndShow(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	first := todayID("alpha-pick")
	second := todayID("beta-pick")
	capture(t, func() int { return RunNew([]string{"chore", "alpha-pick", "第一个任务"}) })
	capture(t, func() int { return RunNew([]string{"chore", "beta-pick", "第二个任务"}) })
	makeReady(t, filepath.Join(root, "backlog", first+".md"))
	makeReady(t, filepath.Join(root, "backlog", second+".md"))
	code, out, err := captureIn(t, "2\n", func() int { return RunPick(nil) })
	if code != 0 {
		t.Fatalf("pick prompt: %s", err)
	}
	if !strings.Contains(out, "1. "+first) || !strings.Contains(out, "2. "+second) {
		t.Fatalf("menu: %s", out)
	}
	if _, e := os.Stat(filepath.Join(root, "todo", second+".md")); e != nil {
		t.Fatal(e)
	}
	code, show, err := capture(t, func() int { return RunShow([]string{second}) })
	if code != 0 || !strings.Contains(show, "# 第二个任务") {
		t.Fatalf("show: %d %q %q", code, show, err)
	}
	empty := tempBoard(t)
	_ = empty
	t.Setenv(EnvBoardDir, empty)
	// empty already has states; pick with no backlog
	for _, state := range States {
		entries, _ := os.ReadDir(filepath.Join(empty, state))
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(empty, state, e.Name()))
		}
	}
	code, _, err = capture(t, func() int { return RunPick(nil) })
	if code == 0 || !strings.Contains(err, "backlog 中没有任务") {
		t.Fatalf("empty pick: %s", err)
	}
}

func TestDoneMetadataKeepsWorking(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("bad-done-metadata")
	capture(t, func() int { return RunNew([]string{"chore", "bad-done-metadata", "任务 bad-done-metadata"}) })
	p := filepath.Join(root, "backlog", taskID+".md")
	makeReady(t, p)
	capture(t, func() int { return RunMove([]string{taskID, "todo"}) })
	capture(t, func() int { return RunMove([]string{taskID, "working"}) })
	p = filepath.Join(root, "working", taskID+".md")
	complete(t, p)
	data, _ := os.ReadFile(p)
	_ = os.WriteFile(p, bytes.Replace(data, []byte("- 完成时间:\n"), []byte("- 完成时间:\n- 完成时间:\n"), 1), 0o644)
	code, _, err := capture(t, func() int { return RunMove([]string{taskID, "done"}) })
	if code == 0 || !strings.Contains(err, "缺少唯一元数据字段: 完成时间") {
		t.Fatalf("dup complete: %s", err)
	}
	if _, e := os.Stat(p); e != nil {
		t.Fatal("should remain in working")
	}
}

func TestEnglishCheck(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	t.Setenv(config.EnvLang, "en")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	code, out, _ := capture(t, func() int { return RunCheck(nil) })
	if code != 0 || out != "ok: 0 tasks\n" {
		t.Fatalf("en check: %d %q", code, out)
	}
	_ = root
}

func TestBoardRootRejectsSymlinkKanbanWithoutWalkingPast(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink locate coverage")
	}
	resetLang(t)
	ancestor := t.TempDir()
	realBoard := filepath.Join(ancestor, "kanban")
	for _, state := range States {
		if err := os.MkdirAll(filepath.Join(realBoard, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(ancestor, "project")
	nested := filepath.Join(project, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, "kanban")); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(t.TempDir(), "KANDER-KANBAN-RULES.md")
	if err := os.WriteFile(rules, []byte("# 全局文件看板规则\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installPathsFn = func() (config.InstallPaths, error) {
		return config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Dir(rules)}, nil
	}
	t.Cleanup(func() { installPathsFn = config.CurrentInstallPaths })
	t.Setenv(EnvBoardDir, "")
	t.Chdir(nested)

	code, _, errb := capture(t, func() int { return RunList(nil) })
	if code == 0 || !strings.Contains(errb, "不得是符号链接/重解析点") {
		t.Fatalf("expected fail-closed on symlink kanban, got code=%d err=%q", code, errb)
	}
	if strings.Contains(errb, realBoard) {
		t.Fatalf("walked past symlink onto ancestor board: %s", errb)
	}
}

func TestInitBoardRejectsSymlinkAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink initialization coverage")
	}
	resetLang(t)
	realParent := t.TempDir()
	linkParent := t.TempDir()
	linked := filepath.Join(linkParent, "linked")
	if err := os.Symlink(realParent, linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvBoardDir, filepath.Join(linked, "kanban"))
	rules := filepath.Join(t.TempDir(), "KANDER-KANBAN-RULES.md")
	if err := os.WriteFile(rules, []byte("# rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installPathsFn = func() (config.InstallPaths, error) {
		return config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Dir(rules)}, nil
	}
	t.Cleanup(func() { installPathsFn = config.CurrentInstallPaths })
	if _, _, _, err := InitBoard(""); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if _, err := os.Stat(filepath.Join(realParent, "kanban")); !os.IsNotExist(err) {
		t.Fatal("initialization wrote through symlink ancestor")
	}
}
