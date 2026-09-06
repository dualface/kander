package review

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/process"
)

func validateContext(agent string, arguments []string) (reviewContext, error) {
	if len(arguments) < 5 || len(arguments) > 7 {
		usage()
		return reviewContext{}, &gateError{code: 2}
	}
	cwdText, base, commit, roleInput, taskInput := arguments[0], arguments[1], arguments[2], arguments[3], arguments[4]
	rawReviewContext := ""
	if len(arguments) >= 6 {
		rawReviewContext = arguments[5]
	}
	reviewCtx := emptyReviewContext
	if strings.TrimSpace(rawReviewContext) != "" {
		reviewCtx = rawReviewContext
	}
	reviewed := ""
	if len(arguments) == 7 && arguments[6] != "" {
		reviewed = arguments[6]
	}
	if reviewed != "" && reviewCtx == emptyReviewContext {
		return reviewContext{}, newGate(2,
			"review.incremental_re_review_requires_the_prior_finding_ledger_in",
		)
	}
	roles := map[string]string{
		"pm": "PM", "qa": "QA", "csa": "CSA",
		"codesecurityanalyst": "CSA", "hacker": "Hacker",
	}
	role, ok := roles[strings.ToLower(roleInput)]
	if !ok {
		return reviewContext{}, newGate(2, "review.unsupported_role", roleInput)
	}
	// Model and reasoning effort can be configured per role, so the role must be settled before the settings are read.
	settings, err := agentSettingsFor(agent, role)
	if err != nil {
		return reviewContext{}, err
	}

	taskSpec := ""
	taskContext := ""
	if looksLikeAbsolutePath(taskInput) {
		info, statErr := os.Stat(taskInput)
		if statErr != nil || info.IsDir() {
			return reviewContext{}, newGate(2, "review.spec_path_is_not_a_readable_file", taskInput)
		}
		file, openErr := os.Open(taskInput)
		if openErr != nil {
			return reviewContext{}, newGate(2, "review.spec_path_is_not_a_readable_file", taskInput)
		}
		_ = file.Close()
		resolved, resErr := filepath.EvalSymlinks(taskInput)
		if resErr != nil {
			return reviewContext{}, newGate(2, "review.could_not_resolve_spec_path", taskInput)
		}
		abs, absErr := filepath.Abs(resolved)
		if absErr != nil {
			return reviewContext{}, newGate(2, "review.could_not_resolve_spec_path", taskInput)
		}
		taskSpec = abs
		taskContext = "Authoritative spec file: " + taskSpec + ". Read it completely before reviewing."
	} else {
		if taskInput == "" {
			return reviewContext{}, newGate(2, "review.task_goal_must_not_be_empty")
		}
		taskContext = "Authoritative task goal: " + taskInput
	}

	if !looksLikeAbsolutePath(cwdText) {
		return reviewContext{}, newGate(2, "review.cwd_must_be_an_absolute_path", cwdText)
	}
	rootOut, _, rootCode, gitErr := gitCommand([]string{"-C", cwdText, "rev-parse", "--show-toplevel"}, "", "")
	if gitErr != nil {
		return reviewContext{}, gitErr
	}
	if rootCode != 0 {
		return reviewContext{}, newGate(2, "review.cwd_is_not_inside_a_git_worktree", cwdText)
	}
	rootResolved, resErr := filepath.EvalSymlinks(strings.TrimRight(rootOut, "\r\n"))
	if resErr != nil {
		return reviewContext{}, newGate(2, "review.cwd_is_not_inside_a_git_worktree", cwdText)
	}
	root, absErr := filepath.Abs(rootResolved)
	if absErr != nil {
		return reviewContext{}, newGate(2, "review.cwd_is_not_inside_a_git_worktree", cwdText)
	}

	tempRoot, err := reviewTempRoot()
	if err != nil {
		return reviewContext{}, err
	}
	home := settings.reviewHome
	var stateRoot string
	homeMissing := false
	if _, statErr := os.Stat(home); os.IsNotExist(statErr) {
		homeMissing = true
	}
	if agent == "cursor" && homeMissing {
		stateRoot = ""
	} else {
		if !dirReadableWritable(home) {
			return reviewContext{}, newGate(2,
				"review.review_home_is_not_readable_and_writable", settings.name, home,
			)
		}
		resolved, _ := filepath.EvalSymlinks(home)
		stateRoot = resolved
	}
	if pathsOverlap(root, tempRoot) || (stateRoot != "" && pathsOverlap(root, stateRoot)) {
		return reviewContext{}, newGate(2,
			"review.worktree_overlaps_a_writable_directory", settings.name, root,
		)
	}

	oidOut, oidErr, oidCode, runErr := gitCommand([]string{"hash-object", "--stdin"}, root, "")
	if runErr != nil {
		return reviewContext{}, runErr
	}
	if oidCode != 0 {
		msg := strings.TrimSpace(oidErr)
		if msg == "" {
			msg = "Git hash-object failed"
		}
		return reviewContext{}, newGateMsg(2, msg)
	}
	oidLength := len(strings.TrimSpace(oidOut))
	shaRe, _ := regexp.Compile("^[0-9a-f]{" + itoa(oidLength) + "}$")
	named := [][2]string{{"base-commit", base}, {"commit", commit}}
	if reviewed != "" {
		named = append(named, [2]string{"reviewed-commit", reviewed})
	}
	for _, item := range named {
		name, oid := item[0], item[1]
		if !shaRe.MatchString(oid) {
			return reviewContext{}, newGate(2, "review.must_be_a_full_commit_sha", name)
		}
		typOut, _, typCode, typErr := gitCommand([]string{"cat-file", "-t", oid}, root, "")
		if typErr != nil {
			return reviewContext{}, typErr
		}
		if typCode != 0 {
			return reviewContext{}, newGate(2, "review.is_not_a_git_object", name, oid)
		}
		if strings.TrimSpace(typOut) != "commit" {
			return reviewContext{}, newGate(2, "review.is_not_a_commit", name, oid)
		}
	}
	_, _, ancCode, ancErr := gitCommand([]string{"merge-base", "--is-ancestor", base, commit}, root, "")
	if ancErr != nil {
		return reviewContext{}, ancErr
	}
	if ancCode != 0 {
		return reviewContext{}, newGate(2, "review.base_commit_is_not_an_ancestor_of_commit")
	}
	if reviewed != "" {
		if reviewed == commit || reviewed == base {
			return reviewContext{}, newGate(2,
				"review.reviewed_commit_must_differ_from_base_commit_and_commit",
			)
		}
		_, _, code, e := gitCommand([]string{"merge-base", "--is-ancestor", base, reviewed}, root, "")
		if e != nil {
			return reviewContext{}, e
		}
		if code != 0 {
			return reviewContext{}, newGate(2,
				"review.base_commit_is_not_an_ancestor_of_reviewed_commit",
			)
		}
		_, _, code, e = gitCommand([]string{"merge-base", "--is-ancestor", reviewed, commit}, root, "")
		if e != nil {
			return reviewContext{}, e
		}
		if code != 0 {
			return reviewContext{}, newGate(2,
				"review.reviewed_commit_is_not_an_ancestor_of_commit",
			)
		}
	}
	headOut, _, headCode, headErr := gitCommand([]string{"rev-parse", "HEAD"}, root, "")
	if headErr != nil {
		return reviewContext{}, headErr
	}
	if headCode != 0 || strings.TrimSpace(headOut) != commit {
		return reviewContext{}, newGate(2, "review.worktree_head_does_not_match_commit")
	}
	statusOK, status := gitStatus(root)
	if !statusOK {
		return reviewContext{}, newGate(2, "review.failed_to_inspect_worktree_status", root)
	}
	if status != "" {
		return reviewContext{}, newGate(2,
			"review.worktree_has_uncommitted_or_untracked_changes", root,
		)
	}
	program := process.ResolveAgentProgram(settings.executable)
	if program == nil {
		return reviewContext{}, newGate(127,
			"review.cli_is_unavailable", settings.name, settings.executable,
		)
	}
	return reviewContext{
		agent:         agent,
		settings:      settings,
		root:          root,
		base:          base,
		commit:        commit,
		role:          role,
		taskContext:   taskContext,
		taskSpec:      taskSpec,
		reviewContext: reviewCtx,
		reviewed:      reviewed,
		program:       *program,
		tempRoot:      tempRoot,
	}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
