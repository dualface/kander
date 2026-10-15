package issue

import (
	"regexp"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/board"
)

// sourceIssueLine is the sibling marker of KANDER-ISSUE-RULES.md. A line
// qualifies after trimming surrounding whitespace and one optional "- " list
// prefix; fenced or not, any other text on the line disqualifies it.
var sourceIssueLine = regexp.MustCompile(`^SOURCE_ISSUE: (\S+) \(anchor: ([a-z0-9-]+)\)$`)

// WorkCard is one card of an issue's work, as seen in one board scan.
type WorkCard struct {
	TaskID string `json:"task_id"`
	State  string `json:"state"`
	Result string `json:"result,omitempty"`
}

// Complete reports whether the card counts as delivered: done, or archived
// with a completed result.
func (c WorkCard) Complete() bool {
	return c.State == "done" || (c.State == "archived" && c.Result == "completed")
}

// Withdrawn reports whether the card left the work without delivering it.
func (c WorkCard) Withdrawn() bool {
	return c.State == "trash" || (c.State == "archived" && c.Result != "completed")
}

// WorkSet is an issue's anchor card plus every valid sibling card. Withdrawn
// siblings are listed in Excluded instead of Siblings. A withdrawn anchor stays
// the anchor and keeps the set from becoming ready.
type WorkSet struct {
	SourceKey string     `json:"source_key"`
	Anchor    WorkCard   `json:"anchor"`
	Siblings  []WorkCard `json:"siblings"`
	Excluded  []WorkCard `json:"excluded"`
}

// Members returns the anchor first, then the siblings in task ID order.
func (w WorkSet) Members() []WorkCard {
	return append([]WorkCard{w.Anchor}, w.Siblings...)
}

// Pending lists the members that are not complete yet.
func (w WorkSet) Pending() []string {
	var pending []string
	for _, card := range w.Members() {
		if !card.Complete() {
			pending = append(pending, card.TaskID)
		}
	}
	return pending
}

// Ready reports whether every member is complete.
func (w WorkSet) Ready() bool { return len(w.Pending()) == 0 }

// Progress returns the complete member count and the member count.
func (w WorkSet) Progress() (done, total int) {
	members := w.Members()
	return len(members) - len(w.Pending()), len(members)
}

// Contains reports whether the task ID is a member or an excluded sibling.
func (w WorkSet) Contains(taskID string) bool {
	for _, card := range append(w.Members(), w.Excluded...) {
		if card.TaskID == taskID {
			return true
		}
	}
	return false
}

// sourceIssueRefs returns the (source key, anchor ID) pairs a card declares.
func sourceIssueRefs(text string) [][2]string {
	discussion, ok := board.SectionBody(text, "DISCUSSION")
	if !ok {
		return nil
	}
	var refs [][2]string
	for _, line := range strings.Split(discussion, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if match := sourceIssueLine.FindStringSubmatch(line); match != nil {
			refs = append(refs, [2]string{match[1], match[2]})
		}
	}
	return refs
}

func workCardOf(entry board.Entry, text string) WorkCard {
	return WorkCard{TaskID: entry.TaskID, State: entry.State, Result: strings.TrimSpace(board.MetadataFrom(text, board.FieldResult))}
}

// workSetsFrom groups the cards of one scan by the anchors of the index. A
// card bound as an anchor is never also a sibling, and a sibling line counts
// only when both its key and its anchor ID name the indexed anchor.
func workSetsFrom(view board.Board, index Index) map[string]WorkSet {
	sets := make(map[string]WorkSet, len(index))
	anchors := make(map[string]struct{}, len(index))
	for key, local := range index {
		anchors[local.TaskID] = struct{}{}
		anchor := WorkCard{TaskID: local.TaskID, State: local.State}
		if text, err := view.Document(local.TaskID); err == nil {
			anchor = workCardOf(view.Entries[local.TaskID], text)
		}
		sets[key] = WorkSet{SourceKey: key, Anchor: anchor}
	}
	for _, id := range sortedTaskIDs(view) {
		if _, isAnchor := anchors[id]; isAnchor {
			continue
		}
		text, err := view.Document(id)
		if err != nil {
			continue
		}
		card := workCardOf(view.Entries[id], text)
		for _, ref := range sourceIssueRefs(text) {
			set, ok := sets[ref[0]]
			if !ok || set.Anchor.TaskID != ref[1] || set.Contains(id) {
				continue
			}
			if card.Withdrawn() {
				set.Excluded = append(set.Excluded, card)
			} else {
				set.Siblings = append(set.Siblings, card)
			}
			sets[ref[0]] = set
		}
	}
	for key, set := range sets {
		sort.Slice(set.Siblings, func(i, j int) bool { return set.Siblings[i].TaskID < set.Siblings[j].TaskID })
		sort.Slice(set.Excluded, func(i, j int) bool { return set.Excluded[i].TaskID < set.Excluded[j].TaskID })
		sets[key] = set
	}
	return sets
}

// LoadWorkSets scans the board once and returns every bound issue's work set,
// keyed by canonical source key.
func LoadWorkSets(root string) (map[string]WorkSet, error) {
	view, err := board.Scan(root)
	if err != nil {
		return nil, err
	}
	return workSetsFrom(view, indexFrom(view)), nil
}
