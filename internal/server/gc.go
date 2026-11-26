package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/servicectl"
)

// gcReceiptBatch is how many idempotency receipts one project-lock hold
// inspects, so a large backlog never blocks the project's requests for long.
var gcReceiptBatch = 1000

// gcCompact is the review run compaction of the board; tests replace it.
var gcCompact = board.CompactReviewRuns

// gcLivenessAge is the age after which a done liveness receipt is removed.
// Its only use is the replay of a late retry, which never comes this late.
const gcLivenessAge = 24 * time.Hour

// gcOptions is a validated gc request.
type gcOptions struct {
	project   string
	dryRun    bool
	olderThan time.Duration
	logKeep   int64
}

// gcTarget is one project a gc works on. lock serializes a receipt batch with
// the project's requests; an offline gc holds the data directory lock and
// needs none.
type gcTarget struct {
	name, root, board string
	lock              func() (unlock func(), err error)
}

// gcRun carries what one gc needs from its host: the running server or the
// offline command that holds the data directory lock.
type gcRun struct {
	data    string
	version string
	now     func() time.Time
	targets []gcTarget
}

// validGC checks a request against the shared limits. A project the registry
// does not know is not_found.
func validGC(req api.ServerGCRequest, known func(string) bool) (gcOptions, error) {
	o := gcOptions{project: req.Project, dryRun: req.DryRun, logKeep: req.LogKeep}
	if req.Project != "" && !api.ValidProjectName(req.Project) {
		return o, apiError(api.CodeInvalidRequest, "invalid gc project name: "+api.ProjectNameRule)
	}
	if req.Project != "" && !known(req.Project) {
		return o, apiError(api.CodeNotFound, "project "+req.Project+" is not registered")
	}
	olderThan, err := time.ParseDuration(req.OlderThan)
	if err != nil || olderThan < command.MinRetention {
		return o, apiError(api.CodeInvalidRequest, "gc older_than must be a Go duration of at least 1h")
	}
	if req.LogKeep < command.MinLogKeep {
		return o, apiError(api.CodeInvalidRequest, "gc log_keep must be at least 1 MiB")
	}
	o.olderThan = olderThan
	return o, nil
}

// gcTargets lists the registered projects in name order; project, when set,
// selects one.
func (s *Server) gcTargets(project string) []gcTarget {
	var targets []gcTarget
	for _, info := range s.projectInfos() {
		if project != "" && info.Name != project {
			continue
		}
		p, err := s.project(info.ID)
		if err != nil {
			continue
		}
		targets = append(targets, gcTarget{name: info.Name, root: p.root, board: p.board, lock: func() (func(), error) {
			if err := p.lockBoard(); err != nil {
				return nil, err
			}
			return p.unlockBoard, nil
		}})
	}
	slices.SortFunc(targets, func(a, b gcTarget) int { return strings.Compare(a.name, b.name) })
	return targets
}

// run plans or performs one gc. Cancellation stops it between batches; the
// log trim always runs to its end once it started.
func (g gcRun) run(ctx context.Context, o gcOptions) api.ServerGCReport {
	report := api.ServerGCReport{DryRun: o.dryRun, Projects: []api.ServerGCProject{}}
	for _, target := range g.targets {
		if err := ctx.Err(); err != nil {
			report.Errors = append(report.Errors, "gc stopped before project "+target.name+": "+err.Error())
			break
		}
		project := api.ServerGCProject{Name: target.name}
		g.compact(ctx, target, o, &project)
		g.expireReceipts(ctx, target, o, &project)
		report.Projects = append(report.Projects, project)
	}
	if o.project == "" && ctx.Err() == nil {
		trim, err := servicectl.TrimLog(g.data, o.logKeep, o.dryRun)
		report.Log = &api.ServerGCLog{Path: trim.Path, Size: trim.Size, Bytes: trim.Bytes}
		if err != nil {
			report.Log.Kept = err.Error()
		}
	}
	return report
}

// gcProtective are the compaction conditions that mark a run needing
// attention. A run that fails only these is a kept candidate; a run that also
// fails a condition of normal operation (4, 5, 6, 8) is no candidate.
var gcProtective = []board.CompactionCondition{board.CompactionRunFinal, board.CompactionPublished, board.CompactionBatchClosed, board.CompactionRunLock, board.CompactionRemoval}

func (g gcRun) compact(ctx context.Context, target gcTarget, o gcOptions, project *api.ServerGCProject) {
	result, err := gcCompact(ctx, target.board, board.CompactionOptions{OlderThan: o.olderThan, Now: g.now(), DryRun: o.dryRun, KanderVersion: g.version})
	project.Compaction.Runs, project.Compaction.Files, project.Compaction.Bytes = len(result.Runs), result.Files, result.Bytes
	for _, skipped := range result.Skipped {
		if len(skipped.Conditions) > 0 && !slices.ContainsFunc(skipped.Conditions, func(c board.CompactionCondition) bool { return !slices.Contains(gcProtective, c) }) {
			project.Compaction.Kept = append(project.Compaction.Kept, api.ServerGCKept{ID: skipped.RunID, Reason: skipped.Reason})
		}
	}
	project.Warnings = append(project.Warnings, result.Warnings...)
	if err != nil {
		project.Errors = append(project.Errors, "review run compaction: "+err.Error())
	}
}

// expireReceipts removes the done idempotency receipts nothing replays any
// more, one batch per project-lock hold. A pending receipt marks an operation
// whose result is uncertain and always stays.
func (g gcRun) expireReceipts(ctx context.Context, target gcTarget, o gcOptions, project *api.ServerGCProject) {
	dir := filepath.Join(target.root, servicePath("receipts"))
	exists, err := fs.DirectoryExists(target.root, dir)
	if err != nil || !exists {
		if err != nil {
			project.Errors = append(project.Errors, "idempotency receipts: "+err.Error())
		}
		return
	}
	entries, err := fs.ListDirectory(target.root, dir)
	if err != nil {
		project.Errors = append(project.Errors, "idempotency receipts: "+err.Error())
		return
	}
	var names []string
	for _, entry := range entries {
		if entry.Kind == fs.KindFile && strings.HasSuffix(entry.Name, ".json") {
			names = append(names, entry.Name)
		}
	}
	slices.Sort(names)
	for start := 0; start < len(names); start += gcReceiptBatch {
		if err := ctx.Err(); err != nil {
			project.Errors = append(project.Errors, "idempotency receipts stopped: "+err.Error())
			return
		}
		if err := g.receiptBatch(target, o, names[start:min(start+gcReceiptBatch, len(names))], project); err != nil {
			project.Errors = append(project.Errors, "idempotency receipts: "+err.Error())
			return
		}
	}
}

func (g gcRun) receiptBatch(target gcTarget, o gcOptions, names []string, project *api.ServerGCProject) error {
	unlock, err := target.lock()
	if err != nil {
		return err
	}
	defer unlock()
	facts := &bindingFacts{board: target.board}
	now := g.now()
	for _, name := range names {
		path := filepath.Join(target.root, servicePath("receipts", name))
		data, found, err := fs.ReadRegularFileIfExists(target.root, path)
		if err != nil || !found {
			if err != nil {
				project.Warnings = append(project.Warnings, "idempotency receipt "+name+": "+err.Error())
			}
			continue
		}
		var r receipt
		if err := json.Unmarshal(data, &r); err != nil {
			project.Warnings = append(project.Warnings, "idempotency receipt "+name+" is damaged: "+err.Error())
			continue
		}
		due, err := receiptDue(r, now, o.olderThan, facts)
		if err != nil {
			return err
		}
		if !due {
			continue
		}
		if !o.dryRun {
			if _, err := fs.RemoveRegularFileIfExists(target.root, path); err != nil {
				project.Receipts.Kept = append(project.Receipts.Kept, api.ServerGCKept{ID: strings.TrimSuffix(name, ".json"), Reason: err.Error()})
				continue
			}
		}
		project.Receipts.Files++
		project.Receipts.Bytes += int64(len(data))
	}
	return nil
}

// livenessReceipt reports an operation the agents and coordinators repeat on
// every observation round; they write receipts but bind no lasting state.
func livenessReceipt(operation string) bool {
	return strings.HasPrefix(operation, "observation:") || strings.HasPrefix(operation, "group-observation:")
}

// receiptBinding is the object a receipt's operation acts on.
type receiptBinding struct {
	kind string // "task", "group", "run", or "" when nothing reliable is bound
	id   string
}

// receiptOperations maps each operation prefix that names one object to the
// kind of that object. The object identity runs to the next ':' or the end.
// Every other operation (new, issue-import, issue-result-save, integration-*,
// review-plan and the other review writes without a run) stays unbound.
var receiptOperations = map[string]string{
	"move": "task", "put": "task", "claim": "task", "takeover": "task", "execution": "task",
	"dispatch": "task", "delivery": "task", "assign": "task", "confirm": "task", "request": "task",
	"answer": "task", "decision-human": "task", "decision-leader": "task", "decision-mode": "task",
	"group-claim": "group", "group-release": "group", "group-reconcile": "group", "group-recover": "group",
	"group-remove-member": "group",
	"review-runtime":      "run", "review-publish": "run",
}

func bindingOf(operation string) receiptBinding {
	prefix, rest, ok := strings.Cut(operation, ":")
	kind := receiptOperations[prefix]
	if !ok || kind == "" {
		return receiptBinding{}
	}
	id, _, _ := strings.Cut(rest, ":")
	if id == "" {
		return receiptBinding{}
	}
	return receiptBinding{kind: kind, id: id}
}

// receiptDue reports whether a gc removes r. A done liveness receipt goes
// after gcLivenessAge. Any other done receipt goes after the retention period
// when its card, every card of its group, or every card of its review run is
// in a terminal state. Receipts that cannot be bound stay.
func receiptDue(r receipt, now time.Time, olderThan time.Duration, facts *bindingFacts) (bool, error) {
	if r.Status != receiptDone || r.CreatedAt.IsZero() {
		return false, nil
	}
	age := now.Sub(r.CreatedAt)
	if livenessReceipt(r.Operation) {
		return age >= gcLivenessAge, nil
	}
	if age < olderThan {
		return false, nil
	}
	binding := bindingOf(r.Operation)
	switch binding.kind {
	case "task":
		return facts.terminal([]string{binding.id})
	case "group":
		members, err := facts.groupMembers(binding.id)
		if err != nil || len(members) == 0 {
			return false, err
		}
		return facts.terminal(members)
	case "run":
		run, err := board.ReadReviewRun(facts.board, binding.id)
		if err != nil || len(run.TaskIDs) == 0 {
			// A run that archive cleanup removed, or one that cannot be read, binds nothing reliable.
			return false, nil
		}
		return facts.terminal(run.TaskIDs)
	}
	return false, nil
}

// bindingFacts reads the board state one receipt batch decides on, once.
type bindingFacts struct {
	board   string
	scanned *board.Board
	groups  map[string][]string
}

func (f *bindingFacts) scan() (*board.Board, error) {
	if f.scanned == nil {
		b, err := board.Scan(f.board)
		if err != nil {
			return nil, err
		}
		f.scanned = &b
	}
	return f.scanned, nil
}

// terminal reports whether every card exists and is in done, archived or
// trash. An unknown or malformed task ID binds nothing reliable.
func (f *bindingFacts) terminal(ids []string) (bool, error) {
	b, err := f.scan()
	if err != nil {
		return false, err
	}
	for _, value := range ids {
		id, err := board.NormalizeTaskID(value)
		if err != nil {
			return false, nil
		}
		entry, ok := b.Entries[id]
		if !ok || (entry.State != "done" && entry.State != "archived" && entry.State != "trash") {
			return false, nil
		}
	}
	return true, nil
}

// groupMembers lists the cards whose TASK_GROUP names group. A card that
// cannot be read makes every group unknown, as compaction treats it.
func (f *bindingFacts) groupMembers(group string) ([]string, error) {
	if f.groups == nil {
		b, err := f.scan()
		if err != nil {
			return nil, err
		}
		groups := map[string][]string{}
		for id, entry := range b.Entries {
			text, err := board.ReadDocument(entry)
			if err != nil {
				f.groups = map[string][]string{}
				return nil, nil
			}
			if g := board.TaskGroupFrom(text); g != "" {
				groups[g] = append(groups[g], id)
			}
		}
		f.groups = groups
	}
	return f.groups[group], nil
}

// gcRoute answers POST /api/v1/server/gc. Only a loopback peer with the
// administration token may run it, and one gc runs at a time. The gc does not
// follow the request: a client that disconnects or times out leaves it to
// finish, and its outcome goes to the server log.
func (s *Server) gcRoute(r *http.Request) outcome {
	if !s.administrator(r) {
		return outcome{err: &api.Error{Code: api.CodeInvalidRequest, Message: "server gc refused", Status: http.StatusForbidden}}
	}
	var req api.ServerGCRequest
	if err := decode(r, &req); err != nil {
		return outcome{err: err}
	}
	o, err := validGC(req, func(name string) bool { _, err := s.projectByName(name); return err == nil })
	if err != nil {
		return outcome{err: err}
	}
	if !s.gcBusy.CompareAndSwap(false, true) {
		return outcome{err: apiError(api.CodeGCRunning, "another gc is running in this server")}
	}
	// close() sets closing before it takes s.mu and waits for gcWorkers, so
	// checking closing under s.mu orders this Add before that Wait.
	s.mu.Lock()
	if s.closing.Load() {
		s.mu.Unlock()
		s.gcBusy.Store(false)
		return outcome{err: closingError()}
	}
	s.gcWorkers.Add(1)
	s.mu.Unlock()
	done := make(chan api.ServerGCReport, 1)
	go func() {
		defer s.gcWorkers.Done()
		defer s.gcBusy.Store(false)
		run := gcRun{data: s.data, version: s.version, now: s.now, targets: s.gcTargets(o.project)}
		report := run.run(s.shutdown, o)
		s.log.Info("gc finished", "dry_run", o.dryRun, "project_name", o.project, "incomplete", report.Incomplete())
		done <- report
	}()
	select {
	case report := <-done:
		return outcome{status: http.StatusOK, body: report}
	case <-r.Context().Done():
		return outcome{err: errors.New("gc client went away; the gc continues in the server")}
	}
}

// OfflineGC holds the data directory lock of a stopped server for one
// command: it plans with a dry run, asks confirm, and then runs the gc. The
// lock is taken without waiting and stays held from the plan to the end, so
// no server starts in between. A held lock returns ErrDataDirInUse.
type OfflineGC struct {
	run  gcRun
	lock *fs.ExclusiveLock
	file interface{ Close() error }
}

// OpenOfflineGC takes the lock of dataDir without waiting and reads the
// project registry. It never recovers boards or writes a control record.
func OpenOfflineGC(dataDir, version string) (*OfflineGC, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, errors.New("server data directory must be an absolute path")
	}
	exists, err := fs.DirectoryExists(filepath.Dir(dataDir), dataDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("server data directory " + dataDir + " does not exist")
	}
	file, err := fs.OpenLockFile(dataDir, filepath.Join(dataDir, servicectl.DataLockName))
	if err != nil {
		return nil, err
	}
	lock, held, err := fs.TryLockExclusive(file)
	if err != nil || !held {
		file.Close()
		if err == nil {
			err = ErrDataDirInUse
		}
		return nil, err
	}
	projects, err := readRegistry(dataDir)
	if err != nil {
		return nil, errors.Join(err, lock.Unlock(), file.Close())
	}
	var targets []gcTarget
	for _, info := range projects {
		targets = append(targets, gcTarget{name: info.Name, root: projectRoot(dataDir, info.Name), board: projectBoardPath(dataDir, info.Name), lock: func() (func(), error) { return func() {}, nil }})
	}
	slices.SortFunc(targets, func(a, b gcTarget) int { return strings.Compare(a.name, b.name) })
	return &OfflineGC{run: gcRun{data: dataDir, version: version, now: func() time.Time { return time.Now().UTC() }, targets: targets}, lock: lock, file: file}, nil
}

// Run validates req like the route does and plans or runs the gc.
func (g *OfflineGC) Run(ctx context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
	o, err := validGC(req, func(name string) bool {
		return slices.ContainsFunc(g.run.targets, func(t gcTarget) bool { return t.name == name })
	})
	if err != nil {
		return api.ServerGCReport{}, err
	}
	run := g.run
	if o.project != "" {
		run.targets = slices.DeleteFunc(slices.Clone(run.targets), func(t gcTarget) bool { return t.name != o.project })
	}
	return run.run(ctx, o), nil
}

// Close releases the data directory lock.
func (g *OfflineGC) Close() error {
	return errors.Join(g.lock.Unlock(), g.file.Close())
}
