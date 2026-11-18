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

func TestOptionValidation(t *testing.T) {
	setupCheckLang(t)
	for _, test := range []struct {
		mode string
		opt  options
		want bool
	}{
		{checkDelivery, options{commit: "HEAD"}, false},
		{checkDelivery, options{base: " ", commit: "HEAD"}, false},
		{checkDelivery, options{base: "HEAD", commit: " "}, false},
		{checkDelivery, options{base: "HEAD", commit: "HEAD"}, true},
		{checkOverlap, options{head: "HEAD"}, false},
		{checkOverlap, options{source: "HEAD", head: ""}, false},
		{checkOverlap, options{source: "HEAD", head: "HEAD"}, true},
	} {
		err := test.opt.validate(test.mode)
		if (err == nil) != test.want || err != nil && err.Code != errCodeUsage {
			t.Fatalf("%s %+v: %+v", test.mode, test.opt, err)
		}
	}
}

func TestArgumentErrorsExitUsage(t *testing.T) {
	setupCheckLang(t)
	for _, args := range [][]string{
		{"delivery", "--base"},
		{"delivery", "--source", "HEAD"},
		{"delivery", "--base", "HEAD", "--base", "HEAD"},
		{"delivery", "--base", "HEAD", "extra"},
	} {
		code, out, errb := captureRun(t, args)
		if code != exitUsage || out != "" || !strings.Contains(errb, "kander check delivery") {
			t.Fatalf("%v code=%d stdout=%q stderr=%q", args, code, out, errb)
		}
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

func TestDeliverySkipsGitlinksAndCountsGitlinkToBlob(t *testing.T) {
	setupCheckLang(t)
	dir := initRepo(t)
	root := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+root+",module")
	git(t, dir, "commit", "-q", "-m", "add gitlink")
	gitlinkCommit := git(t, dir, "rev-parse", "HEAD")
	t.Chdir(dir)

	code, out, errb := captureRun(t, []string{"delivery", "--base", root, "--json"})
	if code != exitOK {
		t.Fatalf("gitlink add code=%d stderr=%s stdout=%s", code, errb, out)
	}
	var result DeliveryResult
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusPass || len(result.AddedOverLimit) != 0 || len(result.CrossedLimit) != 0 {
		t.Fatalf("gitlink add result=%+v", result)
	}

	git(t, dir, "update-index", "--cacheinfo", "160000,"+gitlinkCommit+",module")
	git(t, dir, "commit", "-q", "-m", "update gitlink")
	updatedGitlinkCommit := git(t, dir, "rev-parse", "HEAD")
	code, out, errb = captureRun(t, []string{"delivery", "--base", gitlinkCommit, "--json"})
	if code != exitOK {
		t.Fatalf("gitlink update code=%d stderr=%s stdout=%s", code, errb, out)
	}
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusPass || len(result.AddedOverLimit) != 0 || len(result.CrossedLimit) != 0 {
		t.Fatalf("gitlink update result=%+v", result)
	}

	git(t, dir, "rm", "--cached", "-q", "--", "module")
	if err := os.WriteFile(filepath.Join(dir, "module"), []byte(nLines(1001, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "--", "module")
	git(t, dir, "commit", "-q", "-m", "replace gitlink with blob")
	blobCommit := git(t, dir, "rev-parse", "HEAD")

	code, out, errb = captureRun(t, []string{"delivery", "--base", updatedGitlinkCommit, "--json"})
	if code != exitAction {
		t.Fatalf("gitlink-to-blob code=%d stderr=%s stdout=%s", code, errb, out)
	}
	mustDeliveryJSON(t, out, &result)
	if len(result.CrossedLimit) != 1 {
		t.Fatalf("gitlink-to-blob result=%+v", result)
	}
	item := result.CrossedLimit[0]
	if string(item.Path.Raw) != "module" || item.BaseLines != 0 || item.TargetLines != 1001 {
		t.Fatalf("gitlink-to-blob candidate=%+v", item)
	}

	git(t, dir, "rm", "-q", "--", "module")
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+updatedGitlinkCommit+",module")
	git(t, dir, "commit", "-q", "-m", "replace blob with gitlink")
	code, out, errb = captureRun(t, []string{"delivery", "--base", blobCommit, "--json"})
	if code != exitOK {
		t.Fatalf("blob-to-gitlink code=%d stderr=%s stdout=%s", code, errb, out)
	}
	mustDeliveryJSON(t, out, &result)
	if result.Status != statusPass || len(result.AddedOverLimit) != 0 || len(result.CrossedLimit) != 0 {
		t.Fatalf("blob-to-gitlink result=%+v", result)
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
	wantQuote := runtime.GOOS != "windows"
	if wantQuote {
		// NTFS rejects `"` in a file name; the quoting path is POSIX-only.
		writeCommit(t, dir, `quote"file.txt`, "ok  \n", "quote")
	}
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
	names := []string{"file name.txt", "dash.txt", "trail.txt"}
	if wantQuote {
		names = append(names, "quote")
	}
	for _, name := range names {
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
	requireCheckError(t, code, out, errb, exitExec, "check_invalid_ref")
	absoluteRef := filepath.Join(dir, "private", "missing-ref")
	code, out, errb = captureRun(t, []string{"delivery", "--base", absoluteRef, "--json"})
	requireCheckError(t, code, out, errb, exitExec, "check_invalid_ref")
	if strings.Contains(errb, absoluteRef) {
		t.Fatalf("absolute ref leaked: %s", errb)
	}
	code, out, errb = captureRun(t, []string{"delivery", "--base", second, "--commit", first, "--json"})
	requireCheckError(t, code, out, errb, exitExec, "check_not_ancestor")
}

func TestDeliveryHumanEscapesControls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NTFS cannot store control characters in names")
	}
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

func TestUsageHumanEscapesControls(t *testing.T) {
	setupCheckLang(t)
	argument := "--bad" + string(rune(0x1b)) + "[31m\ninjected" + string(rune(0x9b))
	code, out, errb := captureRun(t, []string{"delivery", argument})
	if code != exitUsage || out != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errb)
	}
	if strings.ContainsRune(errb, rune(0x1b)) || strings.ContainsRune(errb, rune(0x9b)) {
		t.Fatalf("raw control leaked: %q", errb)
	}
	if !strings.Contains(errb, `\x1b[31m\ninjected\u009b`) {
		t.Fatalf("escaped controls missing: %q", errb)
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
		{"delivery", "--base", " ", "--json"},
	}
	for _, args := range cases {
		code, out, errb := captureRun(t, args)
		requireCheckError(t, code, out, errb, exitUsage, "usage")
	}
}

// requireCheckError checks the failure of an analysis that could not finish with --json: the
// exit status, nothing on stdout, and one error object with the code on stderr.
func requireCheckError(t *testing.T, code int, out, errb string, status int, want string) {
	t.Helper()
	if code != status || out != "" {
		t.Fatalf("code=%d (want %d) stdout=%q stderr=%s", code, status, out, errb)
	}
	var object struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if !strings.HasSuffix(errb, "\n") || strings.Count(errb, "\n") != 1 || json.Unmarshal([]byte(errb), &object) != nil {
		t.Fatalf("stderr is not one error object: %q", errb)
	}
	if object.Error.Code != want || object.Error.Message == "" {
		t.Fatalf("error=%+v, want code %q", object.Error, want)
	}
}

func TestInvalidUTF8PathPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("NTFS and APFS both reject invalid UTF-8 names")
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
	requireCheckError(t, code, out, errb, exitExec, "check_not_repository")
}

// Usage text comes from the English command contract in every locale.
func TestUsageIsEnglishInEveryLocale(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
	code, out, _ := captureRun(t, []string{"delivery", "--help"})
	if code != 0 || !strings.Contains(out, "kander check delivery --base <ref>") || !strings.Contains(out, "snippets/commands/check.md") {
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
