package issue

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	rk "github.com/dualface/kander/internal/statemachine/issueresult"
	dk "github.com/dualface/kander/internal/statemachine/issueresultdecision"
)

// The shell of the issue result kernels (formal/KanderFormal/Machines/IssueResult.lean
// and IssueResultDecision.lean). The kernels decide what the result entrances
// accept and refuse; this file only projects a stored record into a kernel
// state and builds the events. The record kernel's instance is one record of
// the store plus three facts about the store (the number of uncertain records,
// whether the issue identity is known, and the record's own counts), so every
// decision about one record holds for a store of any size. The counts are slice
// lengths, which stay below the kernel bound.
var (
	resultKernelParams   = rk.Params{MaxN: math.MaxUint32, MaxId: math.MaxUint32}
	decisionKernelParams = dk.Params{MaxId: math.MaxUint32}
)

// resultID is the identity the kernels compare for a text: the first four bytes
// of its SHA-256, never zero (zero means there is none). Equal texts have equal
// identities; the kernels only ask whether two identities are equal, and the
// validators still ask the shell's exact comparisons.
func resultID(parts ...string) uint64 {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	if id := uint64(binary.BigEndian.Uint32(sum[:4])); id != 0 {
		return id
	}
	return 1
}

func jsonID(kind string, value any) uint64 {
	data, _ := json.Marshal(value)
	return resultID(kind, string(data))
}

// bodyID is the identity of a record's body and author, commentID that of its
// remote comment (zero without one), checksID that of its verification
// evidence, workID that of the work set a record was assessed against or that is
// current, failuresID that of a failure log (zero when empty), and decisionID
// that of a close decision's state version, decision, and user reference.
func bodyID(body string, actor int64) uint64 {
	return resultID("body", body, strconv.FormatInt(actor, 10))
}

func commentID(id int64) uint64 {
	if id <= 0 {
		return 0
	}
	return resultID("comment", strconv.FormatInt(id, 10))
}

func checksID(checks []ResultCheck) uint64 { return jsonID("checks", checks) }

func recordWorkID(record *ResultRecord) uint64 {
	return jsonID("work", []any{record.CardDigest, record.EvidenceVersion, record.TaskIDs, record.Commits})
}

func workID(work ResultWork) uint64 {
	return jsonID("work", []any{work.Digest, work.EvidenceVersion, work.taskIDs(), work.Commits})
}

func failuresID(failures []ResultFailure) uint64 {
	if len(failures) == 0 {
		return 0
	}
	return jsonID("failures", failures)
}

func decisionID(d ResultCloseRecord) uint64 {
	return resultID("decision", d.StateVersion, d.Decision, d.UserReference)
}

// soundRecord is whether a record's author is positive, its outcomes are known,
// and its version is the one its assessment computes.
func soundRecord(record *ResultRecord, taskID string) bool {
	return record.ActorID > 0 && outcomesKnown(record.Outcomes) &&
		resultVersion(record.taskKey(taskID), record.Commits, record.Outcomes, record.FullyResolved) == record.Version
}

func resultCount(n int) uint64 { return min(uint64(n), math.MaxUint32) }

func resultStep(state rk.State, event rk.Event) (rk.State, bool) {
	return rk.Step(state, event, resultKernelParams)
}

func decisionStep(state dk.State, event dk.Event) (dk.State, bool) {
	return dk.Step(state, event, decisionKernelParams)
}

// rstatOf maps a persisted record status to its kernel status; a string the
// store does not write is `other`.
func rstatOf(status string) rk.Rstat {
	switch status {
	case "uncertain":
		return rk.RstatUncertain
	case "published":
		return rk.RstatPublished
	case "equivalent":
		return rk.RstatEquivalent
	case "rejected":
		return rk.RstatRejected
	}
	return rk.RstatOther
}

func statusOf(status rk.Rstat) string {
	switch status {
	case rk.RstatUncertain:
		return "uncertain"
	case rk.RstatPublished:
		return "published"
	case rk.RstatEquivalent:
		return "equivalent"
	case rk.RstatRejected:
		return "rejected"
	}
	return ""
}

// dstatOf maps a persisted close decision status to its kernel status. A
// refusal (`no`) is `declined`; every other status belongs to a consent.
func dstatOf(status string) dk.Dstat {
	switch status {
	case "declined":
		return dk.DstatDeclined
	case "uncertain":
		return dk.DstatUncertain
	case "closed":
		return dk.DstatClosed
	case "observed-closed":
		return dk.DstatObserved
	case "superseded":
		return dk.DstatSuperseded
	case "rejected":
		return dk.DstatRejected
	}
	return dk.DstatOther
}

func decisionStatusOf(status dk.Dstat) string {
	switch status {
	case dk.DstatDeclined:
		return "declined"
	case dk.DstatUncertain:
		return "uncertain"
	case dk.DstatClosed:
		return "closed"
	case dk.DstatObserved:
		return "observed-closed"
	case dk.DstatSuperseded:
		return "superseded"
	case dk.DstatRejected:
		return "rejected"
	}
	return ""
}

func allMet(outcomes []string) bool {
	for _, outcome := range outcomes {
		if outcome != "met" {
			return false
		}
	}
	return true
}

func outcomesKnown(outcomes []string) bool {
	for _, outcome := range outcomes {
		if outcome != "met" && outcome != "unmet" && outcome != "unknown" {
			return false
		}
	}
	return true
}

// projectRecord is the kernel state of the record of `version` in a store: the
// record's status and counts, and the facts of the store around it. A version
// without a record projects to the status `none`.
func projectRecord(store ResultStore, version string, current uint64) rk.State {
	state := rk.State{Iid: store.IssueID > 0, Cw: current}
	for _, record := range store.Records {
		if record != nil && record.Status == "uncertain" {
			state.Unc++
		}
	}
	if record := store.Records[version]; record != nil {
		state.St = rstatOf(record.Status)
		state.Full, state.Met = record.FullyResolved, allMet(record.Outcomes)
		state.Cpos = record.CommentID > 0
		state.Nf, state.Nd = resultCount(len(record.Failures)), resultCount(len(record.Decisions))
		state.Bod, state.Cid, state.Chk, state.Wk = bodyID(record.Body, record.ActorID), commentID(record.CommentID),
			checksID(record.Checks), recordWorkID(record)
		state.Fh = failuresID(record.Failures)
		if n := len(record.Failures); n > 0 {
			state.Fp = failuresID(record.Failures[:n-1])
		}
		state.Sound = soundRecord(record, store.TaskID)
	}
	return state
}

// errResultInconsistent reports a refusal that the shell's own facts rule out.
func errResultInconsistent() error { return resultError("inconsistent result record") }

// validRecord is the structural validity of one stored record that the kernel
// decides: positive remote identities, valid outcomes, a known status. The
// error is the first failing check in the order ValidateResultStore has always
// made them; each check asks the kernel with every other fact holding.
func validRecord(record *ResultRecord) error {
	valid := func(vst rk.Rstat, actorPos, commentPos, outOk, full, met bool) bool {
		_, ok := resultStep(rk.Initial(resultKernelParams), rk.Valid{Vst: vst, ActorPos: actorPos,
			CommentPos: commentPos, OutOk: outOk, Nfull: full, Nmet: met})
		return ok
	}
	vst := rstatOf(record.Status)
	identity := rk.RstatRejected
	if vst == rk.RstatPublished || vst == rk.RstatEquivalent {
		identity = vst
	}
	if !valid(identity, record.ActorID > 0, record.CommentID > 0, true, false, true) {
		return resultError("result records need confirmed numeric remote identities")
	}
	if !valid(rk.RstatRejected, true, true, outcomesKnown(record.Outcomes), record.FullyResolved, allMet(record.Outcomes)) {
		return resultError("invalid stored criterion outcome")
	}
	if !valid(vst, true, true, true, false, true) {
		return resultError("invalid result status")
	}
	return nil
}

// validDecision is the structural validity of one stored close decision.
func validDecision(decision ResultCloseRecord) error {
	if decision.StateVersion == "" || decision.UserReference == "" || decision.Decision != "yes" && decision.Decision != "no" {
		return resultError("invalid close decision")
	}
	valid := func(yes bool, status dk.Dstat) bool {
		_, ok := decisionStep(dk.Initial(decisionKernelParams), dk.Valid{Yes: yes, Vstat: status})
		return ok
	}
	status := dstatOf(decision.Status)
	if !valid(decision.Status != "declined", status) {
		return resultError("invalid close decision status")
	}
	if !valid(decision.Decision == "yes", status) {
		if decision.Decision == "yes" {
			return resultError("a close consent cannot become a refusal")
		}
		return resultError("close refusal cannot authorize a mutation")
	}
	return nil
}

// recordFacts are the comparisons of a record of the old store with the record
// of the new store that the update and work relations ask: the body and author
// are the old ones and a comment, once there is one, is the old one; the
// failures are the old ones or extend them; the verification evidence is the
// old one; the record was assessed against the work set.
type recordFacts struct{ identSame, prefix, checksSame, workSame bool }

func compareRecords(old, current *ResultRecord, work ResultWork) recordFacts {
	return recordFacts{
		identSame: current.Body == old.Body && current.ActorID == old.ActorID &&
			(old.CommentID <= 0 || current.CommentID == old.CommentID),
		prefix: len(current.Failures) >= len(old.Failures) &&
			slices.Equal(current.Failures[:len(old.Failures)], old.Failures),
		checksSame: reflect.DeepEqual(current.Checks, old.Checks),
		workSame: current.CardDigest == work.Digest && current.EvidenceVersion == work.EvidenceVersion &&
			slices.Equal(current.TaskIDs, work.taskIDs()) && slices.Equal(current.Commits, work.Commits),
	}
}

// validRecordUpdate is the update relation of one record of the old store: the
// error is the first failing check in the order ValidateResultStoreUpdate has
// always made them, found by asking the kernel with every other fact holding.
func validRecordUpdate(previous ResultStore, version string, old, current *ResultRecord) error {
	state := projectRecord(previous, version, 0)
	update := func(present bool, status rk.Rstat, identSame bool, failures uint64, prefix bool, decisions uint64) bool {
		_, ok := resultStep(state, rk.Update{Present: present, NewSt: status, IdentSame: identSame, NfNew: failures,
			PrefixF: prefix, NdNew: decisions})
		return ok
	}
	if current == nil {
		return resultError("result history cannot be removed")
	}
	newSt := rstatOf(current.Status)
	facts := compareRecords(old, current, ResultWork{})
	identSame, prefix := facts.identSame, facts.prefix
	failures, decisions := resultCount(len(current.Failures)), resultCount(len(current.Decisions))
	if update(true, newSt, identSame, failures, prefix, decisions) {
		return nil
	}
	switch {
	case !update(true, newSt, true, state.Nf, true, state.Nd):
		return resultError("published result status cannot regress")
	case !update(true, state.St, identSame, state.Nf, true, state.Nd):
		return resultError("published result identity changed")
	case !update(true, state.St, true, failures, prefix, state.Nd):
		return resultError("result failures cannot be removed")
	case !update(true, state.St, true, state.Nf, true, decisions):
		return resultError("close history cannot be removed")
	}
	return errResultInconsistent()
}

// validDecisionUpdate is the update relation of one decision of the old store.
func validDecisionUpdate(old, updated ResultCloseRecord) error {
	identSame := updated.StateVersion == old.StateVersion && updated.Decision == old.Decision &&
		updated.UserReference == old.UserReference
	state := dk.State{Ds: dstatOf(old.Status)}
	if _, ok := decisionStep(state, dk.Settle{NewDs: dstatOf(updated.Status), IdSame: identSame}); ok {
		return nil
	}
	if !identSame {
		return resultError("close decision identity changed")
	}
	return resultError("settled close decision cannot regress")
}

// validRecordWork is the work relation of one record of the new store. The
// error is the first failing check in the order ValidateResultStoreWork has
// always made them, found by asking the kernel with every other fact holding.
func validRecordWork(previous ResultStore, version string, current *ResultRecord, work ResultWork) error {
	old := previous.Records[version]
	state := projectRecord(previous, version, workID(work))
	newSt := rstatOf(current.Status)
	check := func(state rk.State, identical, checksSame, workSame, versionOk bool) bool {
		_, ok := resultStep(state, rk.Work{NewSt: newSt, Identical: identical, ChecksSame: checksSame,
			WorkSame: workSame, VersionOk: versionOk})
		return ok
	}
	if old != nil && old.Status == "rejected" && reflect.DeepEqual(old, current) {
		if check(state, true, false, false, false) {
			return nil
		}
		return errResultInconsistent()
	}
	if old != nil && old.Status != "rejected" {
		if check(state, false, compareRecords(old, current, work).checksSame, false, false) {
			return nil
		}
		return resultError("published verification evidence changed")
	}
	// A new record: absent from the old store, or replacing a rejected one.
	noUncertain := state
	noUncertain.Unc = 0
	if !check(noUncertain, false, false, true, true) {
		return resultError("publication needs its preceding uncertain intent")
	}
	if !check(state, false, false, true, true) {
		return resultError("another publication outcome is still uncertain")
	}
	workSame := compareRecords(&ResultRecord{}, current, work).workSame
	if !check(state, false, false, workSame, true) {
		return resultError("result assessment differs from the current work set")
	}
	calculated, err := proposalVersion(work, ResultProposal{Outcomes: current.Outcomes, Checks: current.Checks,
		FullyResolved: current.FullyResolved, ResolutionEvidence: current.ResolutionEvidence})
	if err != nil {
		return err
	}
	if !check(state, false, false, workSame, calculated == version) {
		return resultError("result version differs from the current assessment")
	}
	return nil
}

// scanDecisions walks the decisions of a record that bind the remote's current
// state version, in order. The first decision that blocks decides the scan.
func scanDecisions(record *ResultRecord, stateVersion string, decision ResultDecision) rk.Scan {
	state := dk.Initial(decisionKernelParams)
	for _, d := range record.Decisions {
		if d.StateVersion != stateVersion {
			continue
		}
		state, _ = decisionStep(state, dk.Visit{Dec: dstatOf(d.Status), Reconsider: decision.Reconsider,
			NewNo: decision.Decision == "no"})
	}
	return rk.Scan(state.Acc)
}
