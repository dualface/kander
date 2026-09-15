package check

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func TestParseOptionsErrors(t *testing.T) {
	setupCheckLang(t)
	_, err := parseOptions(checkDelivery, nil)
	if err == nil || err.Code != errCodeUsage {
		t.Fatalf("missing base: %+v", err)
	}
	_, err = parseOptions(checkDelivery, []string{"--base"})
	if err == nil || err.Code != errCodeUsage {
		t.Fatalf("missing value: %+v", err)
	}
	_, err = parseOptions(checkDelivery, []string{"--source", "HEAD"})
	if err == nil || !strings.Contains(err.Message, "--source") {
		t.Fatalf("unknown for delivery: %+v", err)
	}
	_, err = parseOptions(checkOverlap, []string{"--json", "--json", "--source", "HEAD"})
	if err == nil || err.Code != errCodeUsage {
		t.Fatalf("duplicate json: %+v", err)
	}
	opt, err := parseOptions(checkDelivery, []string{"--json", "--base", "--all"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.base != "--all" || !opt.json {
		t.Fatalf("option-looking ref: %+v", opt)
	}
	opt, err = parseOptions(checkDelivery, []string{"--base", "--json"})
	if err == nil || err.Code != errCodeUsage || !opt.json {
		t.Fatalf("reserved value with json: opt=%+v err=%+v", opt, err)
	}
	opt, err = parseOptions(checkDelivery, []string{"--unknown", "--json"})
	if err == nil || err.Code != errCodeUsage || !opt.json {
		t.Fatalf("unknown option with trailing json: opt=%+v err=%+v", opt, err)
	}
	opt, err = parseOptions(checkDelivery, []string{"--base", "--commit=HEAD", "--json"})
	if err == nil || err.Code != errCodeUsage || !opt.json {
		t.Fatalf("equals reserved value with json: opt=%+v err=%+v", opt, err)
	}
}

func TestDeliveryJSONEmptyDiff(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	t.Chdir(dir)
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "no-board"))
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if errb != "" {
		t.Fatalf("stderr=%q", errb)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusPass || result.Check != checkDelivery || result.SchemaVersion != 1 {
		t.Fatalf("%+v", result)
	}
	if result.Error != nil || result.BaseCommit != base || result.TargetCommit != base {
		t.Fatalf("%+v", result)
	}
	if result.AddedOverLimit == nil || result.CrossedLimit == nil || result.DiffCheck.Diagnostics == nil {
		t.Fatalf("nil arrays %+v", result)
	}
	if strings.Contains(out, dir) {
		t.Fatalf("absolute path leaked: %s", out)
	}
	code2, out2, _ := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code2 != 0 || out2 != out {
		t.Fatalf("json not stable\n%s\n%s", out, out2)
	}
}

func TestDeliveryLineBoundariesAndDelete(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	writeCommit(t, dir, "ok.txt", nLines(1000, true), "add 1000")
	writeCommit(t, dir, "over.txt", nLines(1001, true), "add 1001")
	writeCommit(t, dir, "gone.txt", nLines(1001, true), "add delete-me")
	git(t, dir, "rm", "-q", "gone.txt")
	git(t, dir, "commit", "-q", "-m", "delete")
	writeCommit(t, dir, "taild.txt", nLines(1001, false), "no trailing newline")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusReviewRequired {
		t.Fatalf("status=%s", result.Status)
	}
	if len(result.AddedOverLimit) != 2 {
		t.Fatalf("added=%+v", result.AddedOverLimit)
	}
	paths := map[string]int{}
	for _, item := range result.AddedOverLimit {
		if item.Status != candidateStatus || item.BasePath != nil {
			t.Fatalf("candidate %+v", item)
		}
		paths[string(item.Path.Raw)] = item.TargetLines
	}
	if paths["over.txt"] != 1001 || paths["taild.txt"] != 1001 {
		t.Fatalf("paths=%v", paths)
	}
	if _, ok := paths["gone.txt"]; ok {
		t.Fatal("deleted file was reported")
	}
	if _, ok := paths["ok.txt"]; ok {
		t.Fatal("1000-line file was reported")
	}
}

func TestDeliveryCrossedAndRename(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	writeCommit(t, dir, "small.txt", nLines(900, true), "small")
	writeCommit(t, dir, "keep.txt", nLines(10, true), "keep")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "mv", "small.txt", "renamed.txt")
	if err := os.WriteFile(filepath.Join(dir, "renamed.txt"), []byte(nLines(900, true)+nLines(200, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "--", "renamed.txt")
	git(t, dir, "commit", "-q", "-m", "rename and grow")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if len(result.CrossedLimit) != 1 {
		t.Fatalf("crossed=%+v added=%+v", result.CrossedLimit, result.AddedOverLimit)
	}
	item := result.CrossedLimit[0]
	if string(item.Path.Raw) != "renamed.txt" || item.BasePath == nil || string(item.BasePath.Raw) != "small.txt" {
		t.Fatalf("rename candidate %+v", item)
	}
	if item.BaseLines != 900 || item.TargetLines != 1100 {
		t.Fatalf("lines %+v", item)
	}
}

func TestDeliveryFailOutranksReviewRequired(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	writeCommit(t, dir, "over.txt", nLines(1001, true)+"trailing  \n", "both")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusFail || result.DiffCheck.Status != statusFail {
		t.Fatalf("expected fail over review-required: %+v", result)
	}
	if len(result.AddedOverLimit) == 0 {
		t.Fatal("expected line-count candidates to remain listed")
	}
}

func TestDeliveryDiffCheckFailAndSpecialPaths(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	writeCommit(t, dir, "file name.txt", "ok  \n", "space")
	writeCommit(t, dir, "-dash.txt", "ok  \n", "dash")
	writeCommit(t, dir, `quote"file.txt`, "ok  \n", "quote")
	writeCommit(t, dir, "trail.txt", "hello  \n", "trailing")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusFail || result.DiffCheck.Status != statusFail {
		t.Fatalf("%+v", result)
	}
	joined := strings.Join(result.DiffCheck.Diagnostics, "\n")
	for _, name := range []string{"file name.txt", "dash.txt", "quote", "trail.txt"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("diagnostics missing %q: %q", name, joined)
		}
	}
}

func TestDeliveryInvalidRefAndNotAncestor(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	writeCommit(t, dir, "a.txt", "a\n", "a")
	second := git(t, dir, "rev-parse", "HEAD")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", "--upload-pack=evil", "--json"})
	if code != exitExec {
		t.Fatalf("inject code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if errb != "" {
		t.Fatalf("json stderr=%q", errb)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusError || result.Error == nil || result.Error.Code != errCodeInvalidRef {
		t.Fatalf("%+v", result)
	}
	code, out, errb = captureRun(t, []string{"delivery", "--base", second, "--commit", first, "--json"})
	if code != exitExec {
		t.Fatalf("ancestor code=%d stderr=%s stdout=%s", code, errb, out)
	}
	mustDeliveryJSON(t, out, &result)
	if result.Error == nil || result.Error.Code != errCodeNotAncestor {
		t.Fatalf("%+v", result)
	}
}

func TestDeliveryHumanEscapesControls(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	escName := "weird" + string(rune(0x1b)) + "x.txt"
	c1Name := "c1" + string(rune(0x9b)) + "y.txt"
	writeCommit(t, dir, escName, nLines(1001, true), "esc")
	writeCommit(t, dir, c1Name, nLines(1001, true), "c1")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("raw ESC leaked: %q", out)
	}
	if strings.Contains(out, "\u009b") {
		t.Fatalf("raw C1 leaked: %q", out)
	}
	if !strings.Contains(out, `\x1b`) {
		t.Fatalf("escaped ESC missing: %q", out)
	}
	if !strings.Contains(out, `\u009b`) {
		t.Fatalf("escaped C1 missing: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) < 6 {
		t.Fatalf("expected multi-line report, got %d lines: %q", len(lines), out)
	}
}

func TestOverlapJSON(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	writeCommit(t, dir, "shared.txt", "one\n", "shared")
	writeCommit(t, dir, "only-head.txt", "h\n", "head-only")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-b", "source")
	writeCommit(t, dir, "shared.txt", "source\n", "source-edit")
	writeCommit(t, dir, "only-source.txt", "s\n", "source-only")
	source := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-B", "headbranch", base)
	writeCommit(t, dir, "shared.txt", "head\n", "head-edit")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"overlap", "--source", source, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if errb != "" {
		t.Fatalf("stderr=%q", errb)
	}
	var result OverlapResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != statusActionRequired || result.Check != checkOverlap || result.Error != nil {
		t.Fatalf("%+v", result)
	}
	if len(result.Paths) != 1 || string(result.Paths[0].Raw) != "shared.txt" {
		t.Fatalf("paths=%+v", result.Paths)
	}
	code, out, errb = captureRun(t, []string{"overlap", "--source", git(t, dir, "rev-parse", "HEAD"), "--head", git(t, dir, "rev-parse", "HEAD"), "--json"})
	if code != 0 {
		t.Fatalf("empty overlap code=%d %s %s", code, errb, out)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != statusPass || result.Paths == nil || len(result.Paths) != 0 {
		t.Fatalf("%+v", result)
	}
}

func TestDeliveryCopyUnmodifiedSource(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	writeCommit(t, dir, "origin.txt", nLines(1001, true), "origin")
	base := git(t, dir, "rev-parse", "HEAD")
	copyCommit(t, dir, "origin.txt", "copy.txt", "copy unmodified")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if len(result.AddedOverLimit) != 1 {
		t.Fatalf("added=%+v", result.AddedOverLimit)
	}
	item := result.AddedOverLimit[0]
	if string(item.Path.Raw) != "copy.txt" || item.BasePath == nil || string(item.BasePath.Raw) != "origin.txt" {
		t.Fatalf("copy candidate %+v", item)
	}
	if item.BaseLines != 1001 || item.TargetLines != 1001 {
		t.Fatalf("copy lines %+v", item)
	}
}

func TestOverlapCopyIncludesOldPath(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	writeCommit(t, dir, "origin.txt", nLines(40, true), "origin")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-b", "source")
	writeCommit(t, dir, "origin.txt", nLines(40, true)+"source\n", "source-edit")
	source := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-B", "headbranch", base)
	copyCommit(t, dir, "origin.txt", "copy.txt", "copy unmodified")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"overlap", "--source", source, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result OverlapResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range result.Paths {
		if string(path.Raw) == "origin.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("copy old path missing: %+v", result.Paths)
	}
}

func TestOverlapRenameConsidersBothPaths(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	writeCommit(t, dir, "old.txt", "one\n", "old")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-b", "source")
	writeCommit(t, dir, "old.txt", "source\n", "source-edit")
	source := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-B", "headbranch", base)
	git(t, dir, "mv", "old.txt", "new.txt")
	git(t, dir, "commit", "-q", "-m", "rename")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"overlap", "--source", source, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result OverlapResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range result.Paths {
		if string(path.Raw) == "old.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rename old path missing: %+v", result.Paths)
	}
}

func TestUsageJSON(t *testing.T) {
	setupCheckLang(t)
	cases := [][]string{
		{"delivery", "--json"},
		{"delivery", "--unknown", "--json"},
		{"delivery", "--base", "--json"},
		{"delivery", "--base", "--commit=HEAD", "--json"},
	}
	for _, args := range cases {
		code, out, errb := captureRun(t, args)
		if code != exitUsage {
			t.Fatalf("%v code=%d stderr=%s stdout=%s", args, code, errb, out)
		}
		if errb != "" {
			t.Fatalf("%v stderr=%q", args, errb)
		}
		var result DeliveryResult
		mustDeliveryJSON(t, out, &result)
		if result.Status != statusError || result.Error == nil || result.Error.Code != errCodeUsage {
			t.Fatalf("%v %+v", args, result)
		}
	}
}

func TestHelpAndLegacyUnknownStayCompatible(t *testing.T) {
	setupCheckLang(t)
	code, out, errb := captureRun(t, []string{"--help"})
	if code != 0 || !strings.Contains(out, "kander check delivery") {
		t.Fatalf("help code=%d out=%s err=%s", code, out, errb)
	}
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "missing-board"))
	t.Chdir(t.TempDir())
	code, out, errb = captureRun(t, []string{"not-a-delivery-mode"})
	if code == 0 {
		t.Fatalf("legacy unknown should delegate, code=0 out=%s err=%s", out, errb)
	}
	combined := out + errb
	if strings.Contains(combined, "kander check delivery") || strings.Contains(combined, `"check":"delivery"`) {
		t.Fatalf("unknown mode must not use delivery usage: %q", combined)
	}
	if !strings.Contains(errb, "kander:") {
		t.Fatalf("expected liveness/board error, got %q", errb)
	}
}

func TestInvalidUTF8PathPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NTFS cannot store invalid UTF-8 names")
	}
	setupCheckLang(t)
	dir := initRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	rawName := []byte{0xff, 0xfe, 'z'}
	path := filepath.Join(dir, string(rawName))
	if err := os.WriteFile(path, []byte(nLines(1001, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "--", string(rawName))
	git(t, dir, "commit", "-q", "-m", "invalid utf8")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base, "--json"})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if !strings.Contains(out, `"base64":`) {
		t.Fatalf("expected base64 git path, got %s", out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusReviewRequired || len(result.AddedOverLimit) != 1 {
		t.Fatalf("%+v", result)
	}
	if !bytes.Equal(result.AddedOverLimit[0].Path.Raw, rawName) {
		t.Fatalf("raw path %q", result.AddedOverLimit[0].Path.Raw)
	}
}

func TestNotRepositoryJSON(t *testing.T) {
	setupCheckLang(t)
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	dir := t.TempDir()
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", "HEAD", "--json"})
	if code != exitExec {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Error == nil || result.Error.Code != errCodeNotRepository {
		t.Fatalf("%+v", result)
	}
}

func TestChineseCatalog(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
	code, out, _ := captureRun(t, []string{"delivery", "--help"})
	if code != 0 || !strings.Contains(out, "用法") {
		t.Fatalf("cn help=%q", out)
	}
}

func mustDeliveryJSON(t *testing.T, out string, result *DeliveryResult) {
	t.Helper()
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
		t.Fatalf("json must be one object plus newline: %q", out)
	}
	if err := json.Unmarshal([]byte(out), result); err != nil {
		t.Fatal(err)
	}
	if result.AddedOverLimit == nil || result.CrossedLimit == nil {
		t.Fatal("arrays must decode to empty slices")
	}
}
