package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// gcScan is the board scan the receipt eligibility checks read, which never
// waits for a lock; tests replace it.
var gcScan = board.ScanNoWait

// gcLivenessAge is the age after which a done liveness receipt is removed.
// Its only use is the replay of a late retry, which never comes this late.
const gcLivenessAge = 24 * time.Hour

// errGCProjectBusy reports a project a gc skips because a long-running
// operation, such as a board import, holds it.
var errGCProjectBusy = errors.New("another maintenance operation of this project is running; the project was skipped")

// gcOptions is a validated gc request.
type gcOptions struct {
	project   string
	dryRun    bool
	olderThan time.Duration
	logKeep   int64
}

// gcTarget is one project a gc works on. enter excludes the operations that
// replace the board directory, such as an import, for the whole gc of the
// project; lock serializes a receipt batch with the project's requests. Both
// return at once, holding the lock or failing. An offline gc holds the data
// directory lock and needs neither.
type gcTarget struct {
	name, root, board string
	enter, lock       func() (unlock func(), err error)
}

// tryLockBoard is lockBoard that never waits: a taken mu fails at once with
// not_ready.
func (p *project) tryLockBoard() error {
	if !p.mu.TryLock() {
		return apiError(api.CodeNotReady, "project "+p.info.Name+" was busy; its remaining receipts were skipped")
	}
	if p.server.closing.Load() {
		p.mu.Unlock()
		return closingError()
	}
	if p.importBroken {
		p.mu.Unlock()
		return apiError(api.CodeNotReady, "project "+p.info.Name+" has an unfinished board import; restart the server to recover it")
	}
	return nil
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
		targets = append(targets, gcTarget{name: info.Name, root: p.root, board: p.board,
			enter: func() (func(), error) {
				if !p.maintenance.TryLock() {
					return nil, errGCProjectBusy
				}
				return p.maintenance.Unlock, nil
			},
			lock: func() (func(), error) {
				if err := p.tryLockBoard(); err != nil {
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
		if leave, err := target.enter(); err != nil {
			project.Errors = append(project.Errors, err.Error())
		} else if err := importPending(target.root); err != nil {
			leave()
			project.Errors = append(project.Errors, err.Error())
		} else {
			g.compact(ctx, target, o, &project)
			g.expireReceipts(ctx, target, o, &project)
			leave()
		}
		report.Projects = append(report.Projects, project)
	}
	if o.project == "" && ctx.Err() == nil {
		trim, err := servicectl.TrimLog(g.data, o.logKeep, o.dryRun)
		report.Log = &api.ServerGCLog{Path: trim.Path, Size: trim.Size, Bytes: trim.Bytes, Recovered: trim.Recovered}
		if err != nil {
			report.Log.Kept = err.Error()
		}
	}
	return report
}

// gcProtective are the compaction conditions that mark a run needing
// attention. A run that fails only these is a kept candidate; a run that also
// fails a condition of normal operation (4, 5, 6, 8) is no candidate. A busy
// execution lock of the run or of a settled successor is condition 7.
var gcProtective = []board.CompactionCondition{board.CompactionRunFinal, board.CompactionPublished, board.CompactionBatchClosed, board.CompactionRunLock, board.CompactionRemoval}

// importPending refuses a project whose board import recorded its publication
// intent but did not finish it: its recovery compares the board with the
// digests of the intent, so a gc must not change that board first. The gc
// leaves the recovery to the server that opens the project.
func importPending(root string) error {
	found, err := fs.RegularFileExists(root, filepath.Join(root, "control", "import-pending.json"))
	if err != nil {
		return errors.New("the project was skipped: its board import intent cannot be read: " + err.Error())
	}
	if found {
		return errors.New("the project was skipped: a board import of it waits for recovery; start the server to finish it, then run gc again")
	}
	return nil
}

func (g gcRun) compact(ctx context.Context, target gcTarget, o gcOptions, project *api.ServerGCProject) {
	// NoWait: a board writer that holds the board lock makes the compaction
	// of this project skip and report instead of holding up the other projects.
	result, err := gcCompact(ctx, target.board, board.CompactionOptions{OlderThan: o.olderThan, Now: g.now(), DryRun: o.dryRun, KanderVersion: g.version, NoWait: true})
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
// whose result is uncertain and always stays. Each batch records its set
// before it removes anything; a set an interruption left is finished first.
// None of it waits for a board lock: a board, card, or journal lock another
// process holds skips the rest of the project's receipts with one error.
func (g gcRun) expireReceipts(ctx context.Context, target gcTarget, o gcOptions, project *api.ServerGCProject) {
	dir := filepath.Join(target.root, servicePath("receipts"))
	exists, err := fs.DirectoryExists(target.root, dir)
	if err != nil || !exists {
		if err != nil {
			project.Errors = append(project.Errors, "idempotency receipts: "+err.Error())
		}
		return
	}
	carry, finished, err := g.finishReceiptIntent(ctx, target, o, project)
	if err != nil {
		project.Errors = append(project.Errors, receiptStageError(err))
		return
	}
	names, err := receiptNames(target.root, dir)
	if err != nil {
		project.Errors = append(project.Errors, "idempotency receipts: "+err.Error())
		return
	}
	names = slices.DeleteFunc(names, func(name string) bool { return finished[name] })
	slices.Sort(names)
	for start := 0; start < len(names); start += gcReceiptBatch {
		if err := ctx.Err(); err != nil {
			project.Errors = append(project.Errors, receiptStageError(err))
			return
		}
		if carry, err = g.receiptBatch(target, o, names[start:min(start+gcReceiptBatch, len(names))], carry, project); err != nil {
			project.Errors = append(project.Errors, receiptStageError(err))
			return
		}
	}
}

// receiptStageError is the one project error that ends the receipt stage. A
// held lock names its stable code, so a reader can tell a skip from a failure.
func receiptStageError(err error) string {
	switch {
	case board.LockBusy(err):
		return "idempotency receipts skipped, board.lock_busy: " + err.Error()
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return "idempotency receipts stopped: " + err.Error()
	}
	return "idempotency receipts: " + err.Error()
}

// requireNoPendingReceiptStage refuses while a board transaction waits for
// recovery: its recovery compares the receipt it staged with the recorded
// content, and a removed receipt would stop the project from opening. A
// journal lock another process holds is reported as it is.
func requireNoPendingReceiptStage(boardRoot string) error {
	err := board.RequireNoPendingOperationsNoWait(boardRoot)
	if err == nil || board.LockBusy(err) {
		return err
	}
	return gcPendingTransaction(err)
}

// receiptNames lists the receipt files of dir by name: the receipts of a key
// at the top, and the liveness slots as live/<name>. Other directories are not
// receipts and are left alone.
func receiptNames(root, dir string) ([]string, error) {
	entries, err := fs.ListDirectory(root, dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		switch {
		case entry.Kind == fs.KindFile && strings.HasSuffix(entry.Name, ".json"):
			names = append(names, entry.Name)
		case entry.Kind == fs.KindDirectory && entry.Name == liveReceiptDir:
			slots, err := fs.ListDirectory(root, filepath.Join(dir, liveReceiptDir))
			if err != nil {
				return nil, err
			}
			for _, slot := range slots {
				if slot.Kind == fs.KindFile && strings.HasSuffix(slot.Name, ".json") {
					names = append(names, liveReceiptDir+"/"+slot.Name)
				}
			}
		}
	}
	return names, nil
}

// receiptNamePath is the path of the receipt file a receipt name names.
func receiptNamePath(root, name string) string {
	return filepath.Join(root, servicePath("receipts", filepath.FromSlash(name)))
}

// receiptIntent is the set the receipt batches of a gc remove: written before
// the first removal of a batch, and after its last removal rewritten to the
// members still unresolved, or removed when none is left. Each member keeps
// the retention of the gc that recorded it, so a later gc ends it with that
// retention.
type receiptIntent struct {
	Schema   int                   `json:"schema"`
	Receipts []receiptIntentMember `json:"receipts"`
}

// receiptIntentMember names one receipt by file name, content hash, and size,
// with the retention it was found due under.
type receiptIntentMember struct {
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	OlderThan string `json:"older_than"`
}

// receiptIntentRel is the receipt intent below the project directory.
var receiptIntentRel = filepath.Join("control", "gc-receipts.json")

func receiptIntentPath(root string) string { return filepath.Join(root, receiptIntentRel) }

// finishReceiptIntent completes the set an interrupted gc recorded, in shards
// of at most gcReceiptBatch members per project-lock hold, checking ctx
// between shards. After each shard the intent holds the members that shard
// left unresolved and the members not handled yet, as recorded; a stop
// (cancellation, a busy lock) keeps it so and returns the error.
//
// It returns the unresolved members as carry, and every member it removed or
// carries, so the scan that follows skips them; a replaced member goes to the
// scan. Files and Bytes count only removals; a member ended without one
// counts in Settled.
func (g gcRun) finishReceiptIntent(ctx context.Context, target gcTarget, o gcOptions, project *api.ServerGCProject) (carry []receiptIntentMember, decided map[string]bool, err error) {
	data, found, err := fs.ReadRegularFileIfExists(target.root, receiptIntentPath(target.root))
	if err != nil || !found {
		return nil, nil, err
	}
	var intent receiptIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return nil, nil, errors.New("receipt gc intent is damaged: " + err.Error())
	}
	members := intent.Receipts
	decided = map[string]bool{}
	for next := 0; next < len(members); {
		if next > 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		shard := members[next:min(next+gcReceiptBatch, len(members))]
		handled, left, stop := g.finishIntentShard(target, o, shard, decided, project)
		carry = append(carry, left...)
		next += handled
		if !o.dryRun && handled > 0 {
			if err := settleReceiptIntent(target.root, append(slices.Clone(carry), members[next:]...)); err != nil {
				return nil, nil, errors.Join(stop, err)
			}
		}
		if stop != nil {
			return nil, nil, stop
		}
	}
	return carry, decided, nil
}

// finishIntentShard decides the members of one shard under one project-lock
// hold. It returns how many members it handled from the start of shard, the
// handled members left unresolved, and the error that stopped it, if any.
// An unresolved member keeps its recorded retention: one whose file cannot be
// read, whose removal failed, or whose eligibility check failed for another
// reason than a busy lock.
func (g gcRun) finishIntentShard(target gcTarget, o gcOptions, shard []receiptIntentMember, decided map[string]bool, project *api.ServerGCProject) (handled int, left []receiptIntentMember, err error) {
	unlock, err := target.lock()
	if err != nil {
		return 0, nil, err
	}
	defer unlock()
	if err := requireNoPendingReceiptStage(target.board); err != nil {
		return 0, nil, err
	}
	facts := &bindingFacts{board: target.board}
	now := g.now()
	keep := func(member receiptIntentMember, reason string) {
		project.Receipts.Kept = append(project.Receipts.Kept, api.ServerGCKept{ID: strings.TrimSuffix(member.Name, ".json"), Reason: reason})
		left = append(left, member)
	}
	for i, member := range shard {
		olderThan, err := time.ParseDuration(member.OlderThan)
		if !receiptFileName(member.Name) || err != nil || olderThan < command.MinRetention {
			continue
		}
		path := receiptNamePath(target.root, member.Name)
		data, found, err := fs.ReadRegularFileIfExists(target.root, path)
		if err != nil {
			decided[member.Name] = true
			keep(member, err.Error())
			continue
		}
		if !found {
			decided[member.Name] = true
			project.Receipts.Settled++
			continue
		}
		var r receipt
		if contentHash(data) != member.SHA256 || json.Unmarshal(data, &r) != nil {
			// Replaced or damaged since the set was recorded: the member
			// ends, and the scan decides on the file anew.
			project.Receipts.Settled++
			continue
		}
		due, err := receiptDue(r, now, olderThan, facts)
		switch {
		case board.LockBusy(err):
			// This member and the rest of the shard stay recorded as they are.
			return i, left, err
		case err != nil:
			decided[member.Name] = true
			keep(member, err.Error())
			continue
		case !due:
			project.Receipts.Settled++
			continue
		}
		decided[member.Name] = true
		if !g.removeReceipt(target, o, member.Name, path, data, project) {
			left = append(left, member)
		}
	}
	return len(shard), left, nil
}

// receiptBatch removes the due receipts among names. carry are the members an
// earlier batch left unresolved; they stay recorded with the batch's own
// members but are not handled again, so one project-lock hold handles at most
// len(names) receipts. The members left unresolved after the batch are
// returned. A busy lock ends the batch before anything is recorded or removed.
func (g gcRun) receiptBatch(target gcTarget, o gcOptions, names []string, carry []receiptIntentMember, project *api.ServerGCProject) ([]receiptIntentMember, error) {
	unlock, err := target.lock()
	if err != nil {
		return carry, err
	}
	defer unlock()
	if err := requireNoPendingReceiptStage(target.board); err != nil {
		return carry, err
	}
	facts := &bindingFacts{board: target.board}
	now := g.now()
	type due struct {
		member receiptIntentMember
		path   string
		data   []byte
	}
	var batch []due
	var kept []api.ServerGCKept
	keep := func(name, reason string) {
		kept = append(kept, api.ServerGCKept{ID: strings.TrimSuffix(name, ".json"), Reason: reason})
	}
	for _, name := range names {
		path := receiptNamePath(target.root, name)
		data, found, err := fs.ReadRegularFileIfExists(target.root, path)
		if err != nil {
			keep(name, err.Error())
			continue
		}
		if !found {
			continue
		}
		var r receipt
		if err := json.Unmarshal(data, &r); err != nil {
			keep(name, "damaged receipt: "+err.Error())
			continue
		}
		ok, err := receiptDue(r, now, o.olderThan, facts)
		if board.LockBusy(err) {
			return carry, err
		}
		if err != nil {
			keep(name, err.Error())
			continue
		}
		if ok {
			member := receiptIntentMember{Name: name, SHA256: contentHash(data), Bytes: int64(len(data)), OlderThan: o.olderThan.String()}
			batch = append(batch, due{member, path, data})
		}
	}
	project.Receipts.Kept = append(project.Receipts.Kept, kept...)
	if len(batch) == 0 {
		return carry, nil
	}
	if !o.dryRun {
		intent := receiptIntent{Schema: 1, Receipts: slices.Clone(carry)}
		for _, d := range batch {
			intent.Receipts = append(intent.Receipts, d.member)
		}
		if err := writeReceiptIntent(target.root, intent); err != nil {
			return carry, err
		}
	}
	left := slices.Clone(carry)
	for _, d := range batch {
		if !g.removeReceipt(target, o, d.member.Name, d.path, d.data, project) {
			left = append(left, d.member)
		}
	}
	if o.dryRun {
		return carry, nil
	}
	return left, settleReceiptIntent(target.root, left)
}

// settleReceiptIntent records the members still unresolved, or removes the
// intent when none is left.
func settleReceiptIntent(root string, left []receiptIntentMember) error {
	if len(left) > 0 {
		return writeReceiptIntent(root, receiptIntent{Schema: 1, Receipts: left})
	}
	_, err := fs.RemoveRegularFileIfExists(root, receiptIntentPath(root))
	return err
}

// removeReceipt removes one due receipt, or in a dry run only counts it. It
// reports whether the receipt is gone (or would be).
func (g gcRun) removeReceipt(target gcTarget, o gcOptions, name, path string, data []byte, project *api.ServerGCProject) bool {
	if !o.dryRun {
		if err := receiptRemoveStep(name); err != nil {
			project.Receipts.Kept = append(project.Receipts.Kept, api.ServerGCKept{ID: strings.TrimSuffix(name, ".json"), Reason: err.Error()})
			return false
		}
		if _, err := fs.RemoveRegularFileIfExists(target.root, path); err != nil {
			project.Receipts.Kept = append(project.Receipts.Kept, api.ServerGCKept{ID: strings.TrimSuffix(name, ".json"), Reason: err.Error()})
			return false
		}
	}
	project.Receipts.Files++
	project.Receipts.Bytes += int64(len(data))
	return true
}

// receiptRemoveStep, when a test sets it, runs before each receipt removal
// and may fail it.
var receiptRemoveStep = func(string) error { return nil }

func writeReceiptIntent(root string, intent receiptIntent) error {
	return writeJSON(root, receiptIntentRel, intent)
}

func gcPendingTransaction(err error) error {
	return errors.New("receipts kept while a board transaction waits for recovery; start the server to recover it, then run gc again: " + err.Error())
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// receiptFileName accepts only the name of a receipt file: the hex SHA-256 of
// a key and .json, or a liveness slot, live/ and the same form.
func receiptFileName(name string) bool {
	name = strings.TrimPrefix(name, liveReceiptDir+"/")
	hash, ok := strings.CutSuffix(name, ".json")
	if !ok || len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil && strings.ToLower(hash) == hash
}

// livenessReceipt reports an operation the agents and coordinators repeat on
// every observation round; they write receipts but bind no lasting state.
func livenessReceipt(operation string) bool {
	return strings.HasPrefix(operation, "observation:") || strings.HasPrefix(operation, "group-observation:")
}

// receiptBinding is the object a receipt's operation acts on.
type receiptBinding struct {
	kind string // "task", or "" when nothing reliable is bound
	id   string
}

// receiptOperations maps each operation prefix that names one card to the
// kind of that object. The card ID runs to the next ':' or the end. Every
// other operation stays unbound and its receipt is kept: new, issue-import,
// issue-result-save, integration-*, the review writes, run operations, and the
// group operations except a release (see groupReleaseDue). A group claim,
// reconcile, recovery, or member removal replays as a new write for as long as
// the lease record is active, whatever its members' states, and a run receipt
// binds a run that archive pruning removes with its archived cards.
var receiptOperations = map[string]string{
	"move": "task", "put": "task", "claim": "task", "takeover": "task", "execution": "task",
	"dispatch": "task", "delivery": "task", "assign": "task", "confirm": "task", "request": "task",
	"answer": "task", "decision-human": "task", "decision-leader": "task", "decision-mode": "task",
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

// refusedOnceTerminal are the task operations whose replay a done or trashed
// card refuses: a claim needs todo, a takeover working or review, and a
// confirmation backlog. Every other task operation can still change a done or
// trashed card (assignment, decision mode and records, documents, requests,
// moves to archived or trash, execution and dispatch records), and only an
// archived card refuses every write.
var refusedOnceTerminal = map[string]bool{"claim": true, "takeover": true, "confirm": true}

// receiptDue reports whether a gc removes r. A done liveness receipt goes
// after gcLivenessAge. Any other done receipt goes after the retention period
// once a late retry of it can no longer write: a claim, takeover, or
// confirmation when its card is terminal; any other card operation when its
// card is archived; a group release under groupReleaseDue. Receipts that
// cannot be bound stay. A busy board lock fails with board.lock_busy.
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
	if group, ok := strings.CutPrefix(r.Operation, groupReleasePrefix); ok {
		return groupReleaseDue(r, group, facts)
	}
	binding := bindingOf(r.Operation)
	if binding.kind != "task" {
		return false, nil
	}
	prefix, _, _ := strings.Cut(r.Operation, ":")
	if refusedOnceTerminal[prefix] {
		return facts.inStates([]string{binding.id}, "done", "archived", "trash")
	}
	return facts.inStates([]string{binding.id}, "archived")
}

const groupReleasePrefix = "group-release:"

// groupReleaseDue reports whether the receipt of a successful group release
// may go: the release already set that lease generation to released, so a late
// retry with the old credential can neither release it again nor touch a later
// generation; once every member card of the group is archived or in trash,
// the group ended the way archive pruning settles it (board.PruneArchived), and
// nothing waits for the replay. A failed release (any HTTP status but 200), a
// group with no card on the board, or a board with a card whose text cannot be
// read keeps the receipt.
func groupReleaseDue(r receipt, group string, facts *bindingFacts) (bool, error) {
	if group == "" || r.HTTP != http.StatusOK {
		return false, nil
	}
	return facts.groupEnded(group)
}

// bindingFacts reads the board state one receipt batch decides on, once. The
// scan never waits for a lock: a busy one fails every decision of the batch
// with board.lock_busy, and any other failed scan fails them the same way.
type bindingFacts struct {
	board   string
	scanned *board.Board
	scanErr error
	// groups maps each task group of the scanned cards to whether every
	// member is archived or in trash; unreadable is set when a card's text
	// could not be read, so no group can be known to have ended.
	groups     map[string]bool
	unreadable bool
}

func (f *bindingFacts) scan() (*board.Board, error) {
	if f.scanned == nil && f.scanErr == nil {
		b, err := gcScan(f.board)
		if err != nil {
			f.scanErr = err
		} else {
			f.scanned = &b
		}
	}
	return f.scanned, f.scanErr
}

// groupEnded reports whether the board holds at least one card of group and
// every such card is archived or in trash. The members come from the card
// texts of the same scan; no other read takes a lock.
func (f *bindingFacts) groupEnded(group string) (bool, error) {
	b, err := f.scan()
	if err != nil {
		return false, err
	}
	if f.groups == nil {
		f.groups = map[string]bool{}
		for id, entry := range b.Entries {
			text, err := b.Document(id)
			if err != nil {
				f.unreadable = true
				continue
			}
			g := board.TaskGroupFrom(text)
			if g == "" {
				continue
			}
			ended, seen := f.groups[g]
			f.groups[g] = (ended || !seen) && (entry.State == "archived" || entry.State == "trash")
		}
	}
	return !f.unreadable && f.groups[group], nil
}

// inStates reports whether every card exists and is in one of states. An
// unknown or malformed task ID binds nothing reliable.
func (f *bindingFacts) inStates(ids []string, states ...string) (bool, error) {
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
		if !ok || !slices.Contains(states, entry.State) {
			return false, nil
		}
	}
	return true, nil
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

// noGCLock is the project exclusion of an offline gc: the data directory lock
// it holds already excludes every other writer.
func noGCLock() (func(), error) { return func() {}, nil }

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
		targets = append(targets, gcTarget{name: info.Name, root: projectRoot(dataDir, info.Name), board: projectBoardPath(dataDir, info.Name), enter: noGCLock, lock: noGCLock})
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
