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
	writeCommit(t, dir, "file name.txt", "ok\n", "space")
	writeCommit(t, dir, "-dash.txt", "ok\n", "dash")
	writeCommit(t, dir, `quote"file.txt`, "ok\n", "quote")
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
	if len(result.DiffCheck.Diagnostics) == 0 {
		t.Fatal("missing diagnostics")
	}
	joined := strings.Join(result.DiffCheck.Diagnostics, "\n")
	if !strings.Contains(joined, "trail.txt") {
		t.Fatalf("diagnostics=%q", joined)
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
	name := "weird" + string(rune(0x1b)) + "x.txt"
	writeCommit(t, dir, name, nLines(1001, true), "esc")
	t.Chdir(dir)
	code, out, errb := captureRun(t, []string{"delivery", "--base", base})
	if code != exitAction {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("raw ESC leaked: %q", out)
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.Contains(line, "\n") {
			t.Fatal("nested newline")
		}
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
	code, out, errb := captureRun(t, []string{"delivery", "--json"})
	if code != exitUsage {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errb, out)
	}
	if errb != "" {
		t.Fatalf("stderr=%q", errb)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusError || result.Error == nil || result.Error.Code != errCodeUsage {
		t.Fatalf("%+v", result)
	}
}

func TestHelpAndLegacyUnknownStayCompatible(t *testing.T) {
	setupCheckLang(t)
	code, out, errb := captureRun(t, []string{"--help"})
	if code != 0 || !strings.Contains(out, "kander check delivery") {
		t.Fatalf("help code=%d out=%s err=%s", code, out, errb)
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
