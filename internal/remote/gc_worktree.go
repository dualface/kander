package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// gcRepository caches what the worktree category learns about one main
// repository during a pass: the remote integration targets it resolved and
// whether a worktree was removed from it (so it is pruned once).
type gcRepository struct {
	targets map[string]gcTarget
	pruned  bool
	// origin is why the repository's origin is not the project's common
	// remote, or nil once it was verified; checked says it was looked at.
	origin  error
	checked bool
}

// gcTarget is the remote integration target of one repository: the commit
// refs/remotes/origin/<target> names after a fetch (or what ls-remote lists
// in a dry run), and whether that commit is present locally.
type gcTarget struct {
	commit  string
	present bool
	err     error
}

// gcWorktree is one generation's worktree candidate while it is judged.
type gcWorktree struct {
	id     string
	task   string
	intent ExecutionWorktree
}

// collectWorktrees removes the worktree and the task branch of each generation
// of a terminal card that passes every condition of the client gc contract,
// then its intent record, the emptied task directories, and prunes Git's
// worktree registry once per repository.
func (r *gcRun) collectWorktrees(ctx context.Context) {
	repos := map[string]*gcRepository{}
	evidence := map[string][]string{}
	r.resumeRemovals(ctx, repos)
	intents := filepath.Join(r.root, "worktree-intents")
	for _, task := range r.listTaskDirectories(intents) {
		summary, ok := r.terminalTask(task)
		if !ok {
			continue
		}
		entries, err := fs.ListDirectory(r.s.Home, filepath.Join(intents, task))
		if err != nil {
			r.fail(err)
			continue
		}
		for _, entry := range entries {
			generation, ok := intentGeneration(entry)
			if !ok {
				continue
			}
			r.collectWorktree(ctx, summary, generation, filepath.Join(intents, task, entry.Name), repos, evidence)
		}
		r.removeEmptyDirectory(filepath.Join(intents, task))
	}
	worktrees := filepath.Join(r.root, "worktrees")
	for _, task := range r.listTaskDirectories(worktrees) {
		if _, ok := r.terminalTask(task); ok {
			r.removeEmptyDirectory(filepath.Join(worktrees, task))
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(repos)) {
		if repos[dir].pruned {
			if _, err := r.git(ctx, dir, "worktree", "prune"); err != nil {
				r.fail(err)
			}
		}
	}
}

// listTaskDirectories lists the task directories below dir, sorted; a missing
// dir has none.
func (r *gcRun) listTaskDirectories(dir string) []string {
	entries, err := fs.ListDirectory(r.s.Home, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.fail(err)
		return nil
	}
	var tasks []string
	for _, entry := range entries {
		if entry.Kind == fs.KindDirectory && api.ValidIdentity(entry.Name) {
			tasks = append(tasks, entry.Name)
		}
	}
	slices.Sort(tasks)
	return tasks
}

// removeEmptyDirectory removes dir when it holds nothing. A dry run only
// notes that there is such a directory to remove.
func (r *gcRun) removeEmptyDirectory(dir string) {
	entries, err := fs.ListDirectory(r.s.Home, dir)
	if errors.Is(err, os.ErrNotExist) || err == nil && len(entries) > 0 {
		return
	}
	if err == nil && r.opts.DryRun {
		r.work = true
		return
	}
	if err == nil {
		_, err = fs.RemoveEmptyDirectoryIfExists(r.s.Home, dir)
	}
	if err != nil {
		r.fail(err)
	}
}

// within reports whether path is dir or below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// physicallyWithin reports whether path is dir or below it, as written or
// after resolving symbolic links on both sides: a working directory entered
// through a link has a logical path that names another directory.
func physicallyWithin(path, dir string) (bool, error) {
	if within(path, dir) {
		return true, nil
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	return within(realPath, realDir), nil
}

// intentGeneration reads the generation of a g<n>.json intent record name.
func intentGeneration(entry fs.DirEntry) (uint64, bool) {
	digits, ok := strings.CutSuffix(strings.TrimPrefix(entry.Name, "g"), ".json")
	if entry.Kind != fs.KindFile || !ok || !strings.HasPrefix(entry.Name, "g") || digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	generation, err := strconv.ParseUint(digits, 10, 64)
	return generation, err == nil && generation > 0
}

// collectWorktree judges one generation. The card being terminal and older
// than the retention period make it a candidate; every later condition that
// fails keeps it with the reason.
func (r *gcRun) collectWorktree(ctx context.Context, summary api.TaskSummary, generation uint64, intentPath string, repos map[string]*gcRepository, evidence map[string][]string) {
	w := gcWorktree{id: fmt.Sprintf("%s/g%d", summary.TaskID, generation), task: summary.TaskID}
	data, err := fs.ReadRegularFile(r.s.Home, intentPath)
	if err == nil {
		err = json.Unmarshal(data, &w.intent)
	}
	expected := filepath.Join(r.root, "worktrees", w.task, fmt.Sprintf("g%d", generation))
	if err != nil || w.intent.Generation != generation || filepath.Clean(w.intent.Path) != expected || !api.ValidIdentity(w.intent.ExecutionID) {
		r.report.Worktrees.keep(w.id, config.Text("gc.client_intent_damaged", intentPath))
		return
	}
	exists, err := fs.DirectoryExists(r.s.Home, expected)
	if err != nil {
		r.report.Worktrees.keep(w.id, err.Error())
		return
	}
	if !exists {
		return
	}
	finished, ok := cardTime(summary.CompletedAt)
	if !ok {
		info, err := os.Lstat(expected)
		if err != nil {
			r.report.Worktrees.keep(w.id, err.Error())
			return
		}
		finished = info.ModTime()
	}
	if !finished.Before(r.cutoff) {
		return
	}
	if r.opts.Checkout != "" {
		inside, err := physicallyWithin(r.opts.Checkout, expected)
		if err != nil {
			r.report.Worktrees.keep(w.id, config.Text("gc.client_inside_unknown", err.Error()))
			return
		}
		if inside {
			r.report.Worktrees.keep(w.id, config.Text("gc.client_inside"))
			return
		}
	}
	if !api.KnownExecutionBranch(w.intent.Branch, r.s.Info.ID, w.task, generation) {
		r.report.Worktrees.keep(w.id, config.Text("gc.client_branch_name", w.intent.Branch))
		return
	}
	lockPath := filepath.Join(credentialDir(r.s.Home, r.s.ServerURL, r.s.Info.ID, w.task), "worktree-"+w.intent.ExecutionID+".lock")
	release, held, err := r.tryLock(lockPath)
	if err != nil {
		r.report.Worktrees.keep(w.id, err.Error())
		return
	}
	if !held {
		r.report.Worktrees.keep(w.id, config.Text("gc.client_locked", lockPath))
		return
	}
	defer func() {
		if err := release(); err != nil {
			r.fail(err)
		}
	}()
	reason, deferred, repo := r.judgeWorktree(ctx, w, repos, evidence)
	switch {
	case deferred:
		r.report.Worktrees.Deferred = append(r.report.Worktrees.Deferred, GCEntry{ID: w.id, Reason: reason})
		return
	case reason != "":
		r.report.Worktrees.keep(w.id, reason)
		return
	}
	size := treeSize(expected)
	if r.opts.DryRun {
		r.report.Worktrees.add(size)
		r.report.Branches.add(0)
		return
	}
	if r.removeWorktree(ctx, w, repo, repos) {
		r.report.Worktrees.add(size)
	}
}

// judgeWorktree checks the conditions that need the held lock: the card read
// again, the claim and credential of this host, the executors, the Git state
// of the worktree and its branch tip. It returns the reason to keep the
// worktree (empty when it may go), whether a dry run defers it to the fetch of
// a real run, and the main repository it belongs to.
func (r *gcRun) judgeWorktree(ctx context.Context, w gcWorktree, repos map[string]*gcRepository, evidence map[string][]string) (reason string, deferred bool, repo string) {
	card, err := r.s.Project.Show(ctx, w.task)
	if err != nil {
		r.fail(err)
		return err.Error(), false, ""
	}
	if !gcTerminalState(card.State) {
		return config.Text("gc.client_card_changed", card.State), false, ""
	}
	if reason := r.localExecution(ctx, w, card); reason != "" {
		return reason, false, ""
	}
	repo, reason = r.worktreeRepository(ctx, w)
	if reason != "" {
		return reason, false, ""
	}
	if _, found := repos[repo]; !found {
		repos[repo] = &gcRepository{targets: map[string]gcTarget{}}
	}
	if r.repository == "" {
		r.repository = repo
	}
	status, err := r.git(ctx, w.intent.Path, "--no-optional-locks", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err.Error(), false, repo
	}
	if status != "" {
		return config.Text("gc.client_dirty"), false, repo
	}
	tip, err := r.git(ctx, w.intent.Path, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return err.Error(), false, repo
	}
	if tip == w.intent.Base {
		return "", false, repo
	}
	// Integration and delivery evidence count only from the project's own
	// common remote: a clone whose origin points elsewhere proves nothing.
	if cache := repos[repo]; !cache.checked {
		cache.checked = true
		if r.s.Info.Remote == "" {
			cache.origin = errors.New(config.Text("remote.git_remote_mismatch"))
		} else {
			cache.origin = verifiedRemote(ctx, repo, r.s.Info.Remote)
		}
	}
	if err := repos[repo].origin; err != nil {
		return config.Text("gc.client_origin_unverified", repo, err.Error()), false, repo
	}
	target, err := board.IntegrationTargetOf(card.Text)
	if err != nil {
		return err.Error(), false, repo
	}
	if target == "" {
		target = r.opts.Mainline
	}
	resolved := r.integrationTarget(ctx, repo, target, repos[repo])
	switch {
	case resolved.err != nil:
		return resolved.err.Error(), false, repo
	case resolved.commit != "" && !resolved.present:
		return config.Text("gc.client_fetch_deferred", target), true, repo
	case resolved.commit != "":
		contained, err := r.ancestor(ctx, repo, tip, resolved.commit)
		if err != nil && r.opts.DryRun {
			// Without lazy fetch a partial clone may miss an object the
			// ancestry needs; the run fetches the target first.
			return config.Text("gc.client_fetch_deferred", target), true, repo
		}
		if err != nil {
			return err.Error(), false, repo
		}
		if contained {
			return "", false, repo
		}
	}
	if card.Delivery != nil && card.Delivery.Commit == tip {
		refs, err := r.taskEvidence(ctx, repo, w.task, evidence)
		if err != nil {
			return err.Error(), false, repo
		}
		if slices.Contains(refs, tip) {
			return "", false, repo
		}
	}
	return config.Text("gc.client_unintegrated", tip, target), false, repo
}

// localExecution is the reason to keep a worktree while this host can still
// act for the generation: a standing claim, an unreleased credential, or a
// retained executor the probe does not see stopped.
func (r *gcRun) localExecution(ctx context.Context, w gcWorktree, card api.Task) string {
	if c := card.Claim; c != nil && api.ClaimStands(c.Status) && c.ClientID == r.s.Client.ClientID() && c.Generation == w.intent.Generation {
		return config.Text("gc.client_claim_valid")
	}
	credential := filepath.Join(credentialDir(r.s.Home, r.s.ServerURL, r.s.Info.ID, w.task), w.intent.ExecutionID+".json")
	held, err := fs.RegularFileExists(r.s.Home, credential)
	if err != nil {
		return err.Error()
	}
	if held {
		return config.Text("gc.client_credential", credential)
	}
	executions, err := r.retainedExecutions(w)
	if err != nil {
		return err.Error()
	}
	if len(executions) == 0 {
		return ""
	}
	statuses := make([]string, len(executions))
	if r.opts.Probe != nil {
		statuses = r.opts.Probe(ctx, executions)
	}
	for i, execution := range executions {
		if i >= len(statuses) || statuses[i] != "stopped" {
			status := "unknown"
			if i < len(statuses) && statuses[i] != "" {
				status = statuses[i]
			}
			return config.Text("gc.client_executor", execution.Window+" "+status, w.task)
		}
	}
	return ""
}

// retainedExecutions lists the retained executions of this generation that
// record an executor. One that records neither a window nor a session had no
// executor Kander started, so there is nothing to probe.
func (r *gcRun) retainedExecutions(w gcWorktree) ([]RetainedExecution, error) {
	dir := filepath.Join(r.s.Home, "retained-executions", projectNamespace(r.s.ServerURL, r.s.Info.ID), w.task)
	entries, err := fs.ListDirectory(r.s.Home, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []RetainedExecution
	for _, entry := range entries {
		if entry.Kind != fs.KindFile || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		data, err := fs.ReadRegularFile(r.s.Home, filepath.Join(dir, entry.Name))
		if err != nil {
			return nil, err
		}
		var value RetainedExecution
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, entry.Name), err)
		}
		if value.Generation != w.intent.Generation && filepath.Clean(value.Worktree) != filepath.Clean(w.intent.Path) {
			continue
		}
		if value.Window != "" || value.Session != "" {
			out = append(out, value)
		}
	}
	return out, nil
}

// worktreeRepository finds the main repository of the worktree and checks
// that Git registers the worktree there with the recorded branch checked out.
// When the command runs in a checkout, the worktree must share its common Git
// directory.
func (r *gcRun) worktreeRepository(ctx context.Context, w gcWorktree) (repo, reason string) {
	common, err := r.git(ctx, w.intent.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", config.Text("gc.client_not_worktree", w.intent.Path)
	}
	if r.opts.Checkout != "" {
		own, err := r.git(ctx, r.opts.Checkout, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return "", err.Error()
		}
		if filepath.Clean(own) != filepath.Clean(common) {
			return "", config.Text("gc.client_repository_mismatch", common, own)
		}
	}
	listing, err := r.git(ctx, w.intent.Path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", err.Error()
	}
	registered := false
	for line := range strings.SplitSeq(listing, "\x00") {
		path, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		if repo == "" {
			repo = path
		}
		if filepath.Clean(path) == filepath.Clean(w.intent.Path) {
			registered = true
		}
	}
	if repo == "" || !registered || filepath.Clean(repo) == filepath.Clean(w.intent.Path) {
		return "", config.Text("gc.client_not_worktree", w.intent.Path)
	}
	head, err := r.git(ctx, w.intent.Path, "symbolic-ref", "-q", "HEAD")
	if err != nil || head != "refs/heads/"+w.intent.Branch {
		return "", config.Text("gc.client_head_mismatch", head, w.intent.Branch)
	}
	return repo, ""
}

// integrationTarget resolves origin's integration target once per repository.
// A real run fetches it first; a dry run only lists it and reports whether the
// commit is already present locally, so its ancestry can be judged.
func (r *gcRun) integrationTarget(ctx context.Context, repo, target string, cache *gcRepository) gcTarget {
	if cached, found := cache.targets[target]; found {
		return cached
	}
	var resolved gcTarget
	if r.opts.DryRun {
		listed, err := r.git(ctx, repo, "ls-remote", "--refs", "origin", "refs/heads/"+target)
		if err != nil {
			resolved.err = err
		} else if fields := strings.Fields(listed); len(fields) >= 2 && fields[1] == "refs/heads/"+target && api.CommitSHA(fields[0]) {
			resolved.commit = fields[0]
			_, err := r.git(ctx, repo, "cat-file", "-e", resolved.commit+"^{commit}")
			resolved.present = err == nil
		}
	} else {
		ref := "refs/remotes/origin/" + target
		// Only the one tracking ref may change: no tags follow, no configured
		// refmap updates another ref, and nothing is pruned.
		if _, err := r.git(ctx, repo, "-c", "fetch.prune=false", "-c", "remote.origin.prune=false", "-c", "fetch.pruneTags=false", "-c", "remote.origin.pruneTags=false",
			"fetch", "--quiet", "--no-tags", "--no-prune", "--refmap=", "origin", "+refs/heads/"+target+":"+ref); err != nil {
			resolved.err = err
		} else if commit, err := r.git(ctx, repo, "rev-parse", "--verify", ref+"^{commit}"); err != nil {
			resolved.err = err
		} else {
			resolved.commit, resolved.present = commit, true
		}
	}
	cache.targets[target] = resolved
	return resolved
}

// taskEvidence lists the commits the common remote holds below the evidence
// prefix of one card, once per pass.
func (r *gcRun) taskEvidence(ctx context.Context, repo, task string, cache map[string][]string) ([]string, error) {
	if refs, found := cache[task]; found {
		return refs, nil
	}
	listed, err := r.git(ctx, repo, "ls-remote", "--refs", "origin", taskEvidencePrefix(r.s.Info.ID, task)+"*")
	if err != nil {
		return nil, err
	}
	var commits []string
	for line := range strings.Lines(listed) {
		if fields := strings.Fields(line); len(fields) >= 2 && api.CommitSHA(fields[0]) {
			commits = append(commits, fields[0])
		}
	}
	cache[task] = commits
	return commits, nil
}
