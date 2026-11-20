package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
)

// maxMaterialText is the longest string value of a publication material that
// `kander review publication` prints in full, counted in characters.
const maxMaterialText = 4096

// publicationView is what `kander review publication` prints: the stored
// material of a joint publication, made safe to show to a holder.
type publicationView struct {
	PublicationID string                  `json:"publication_id"`
	Operation     string                  `json:"operation"`
	Status        string                  `json:"status"`
	Digest        string                  `json:"digest"`
	CreatedAt     time.Time               `json:"created_at"`
	Sanitized     bool                    `json:"sanitized"`
	Members       []publicationMemberView `json:"members"`
	Payload       any                     `json:"payload"`
}

type publicationMemberView struct {
	TaskID     string `json:"task_id"`
	Revision   uint64 `json:"revision"`
	Generation uint64 `json:"generation"`
	Confirmed  bool   `json:"confirmed"`
	Stale      bool   `json:"stale"`
}

// confirmResult is what `kander review confirm` prints. Pending lists the
// members that still lack a confirmation, so a holder sees at once whether its
// confirmation completed the set.
type confirmResult struct {
	PublicationID string   `json:"publication_id"`
	Operation     string   `json:"operation"`
	TaskID        string   `json:"task_id"`
	Status        string   `json:"status"`
	Digest        string   `json:"digest"`
	Pending       []string `json:"pending"`
}

// confirmPublication confirms the member task of a publication as its holder and reports
// which members still lack a confirmation.
func confirmPublication(ctx context.Context, session *remote.Session, id, task string) (confirmResult, error) {
	publication, err := session.ConfirmReviewPublication(ctx, id, task)
	if err != nil {
		return confirmResult{}, err
	}
	return confirmResult{PublicationID: publication.ID, Operation: publication.Operation, TaskID: task, Status: "confirmed", Digest: publication.Digest, Pending: remote.UnconfirmedMembers(publication)}, nil
}

// readPublicationView reads one publication and every member's current card.
// It only reads: no credential is sent and nothing is created or changed. The
// material is written by the orchestrator, so every string of it is untrusted
// text and is cleaned before it can reach a terminal.
func readPublicationView(ctx context.Context, session *remote.Session, id string) (publicationView, error) {
	publication, err := session.Project.Publication(ctx, id)
	if err != nil {
		return publicationView{}, err
	}
	var text materialText
	view := publicationView{
		PublicationID: text.clean(publication.ID),
		Operation:     text.clean(publication.Operation),
		Status:        text.clean(publication.Status),
		Digest:        text.clean(publication.Digest),
		CreatedAt:     publication.CreatedAt,
		Members:       []publicationMemberView{},
	}
	for _, member := range publication.Members {
		stale, err := session.MemberStale(ctx, member)
		if err != nil {
			return publicationView{}, err
		}
		view.Members = append(view.Members, publicationMemberView{TaskID: text.clean(member.TaskID), Revision: member.Revision, Generation: member.Generation, Confirmed: remote.MemberConfirmed(publication, member), Stale: stale})
	}
	if view.Payload, err = text.payload(publication); err != nil {
		return publicationView{}, err
	}
	view.Sanitized = text.replaced
	return view, nil
}

// materialText cleans the strings of a material and remembers whether it
// replaced any character.
type materialText struct{ replaced bool }

// ansiSequence matches a complete CSI or OSC escape sequence, in its 7-bit form (ESC [ and ESC ])
// and in its 8-bit form (U+009B and U+009D). An OSC ends at BEL, at the 8-bit ST (U+009C) or at
// the 7-bit ST (ESC \), and its text holds none of them, nor CAN or SUB, which cancel a sequence,
// nor an ESC that starts another one. A sequence that is cancelled or never ends is no complete
// sequence: its introducer is replaced as a control character and the text after it stays.
var ansiSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x{9b}[0-?]*[ -/]*[@-~]|(?:\x1b\]|\x{9d})[^\x07\x1b\x18\x1a\x{9c}]*(?:\x07|\x{9c}|\x1b\\)`)

// clean replaces every escape sequence with one marker of its introducer, and every control or
// bidirectional override character with its own <U+XXXX> marker. Newline and tab stay.
func (m *materialText) clean(value string) string { return m.cleanPrefix(value, -1) }

// cleanPrefix is clean for the first limit characters of value; a negative limit takes all of it.
// Sequences are found in the whole value, so one that starts inside the prefix and ends after it
// is one marker and leaves none of its text behind.
func (m *materialText) cleanPrefix(value string, limit int) string {
	if limit < 0 && strings.IndexFunc(value, unsafeRune) < 0 {
		return value
	}
	sequences := ansiSequence.FindAllStringIndex(value, -1)
	var out strings.Builder
	for at, taken := 0, 0; at < len(value) && (limit < 0 || taken < limit); {
		if len(sequences) > 0 && sequences[0][0] == at {
			end := sequences[0][1]
			r, _ := utf8.DecodeRuneInString(value[at:])
			m.mark(&out, r)
			taken += utf8.RuneCountInString(value[at:end])
			at, sequences = end, sequences[1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(value[at:])
		if unsafeRune(r) {
			m.mark(&out, r)
		} else {
			out.WriteRune(r)
		}
		at += size
		taken++
	}
	return out.String()
}

func (m *materialText) mark(out *strings.Builder, r rune) {
	m.replaced = true
	fmt.Fprintf(out, "<U+%04X>", r)
}

// unsafeRune reports a character that can change what a terminal or a reader
// shows: C0 controls except newline and tab, DEL, C1 controls, and the
// bidirectional overrides and marks.
func unsafeRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x200e || r == 0x200f || r == 0x061c
}

// payload decodes the stored material to generic JSON and cleans it. File
// contents are never printed: the originals of a review-prepare material become
// a name, a length and a digest each.
func (m *materialText) payload(publication api.Publication) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(publication.Payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("publication material is not JSON: %w", err)
	}
	object, ok := value.(map[string]any)
	if !ok || publication.Operation != "review-prepare" {
		return m.value(value), nil
	}
	raw, present := object["originals"]
	files, err := summarizeOriginals(raw)
	if err != nil {
		return nil, err
	}
	delete(object, "originals")
	cleaned := m.object(object)
	if present {
		cleaned["originals"] = m.originals(files)
	}
	return cleaned, nil
}

type originalSummary struct {
	name   string
	bytes  int
	sha256 string
}

// summarizeOriginals reads the file map of a review-prepare material, which
// holds each file as base64 text, and keeps the name, the length and the digest
// of every file in the order of the names.
func summarizeOriginals(value any) ([]originalSummary, error) {
	if value == nil {
		return nil, nil
	}
	files, ok := value.(map[string]any)
	if !ok {
		return nil, archiveError("publication material has invalid originals")
	}
	list := make([]originalSummary, 0, len(files))
	for _, name := range slices.Sorted(maps.Keys(files)) {
		// A file the client left empty is a nil byte slice, which JSON writes as null.
		var data []byte
		if files[name] != nil {
			encoded, ok := files[name].(string)
			if !ok {
				return nil, archiveError("publication material has invalid originals")
			}
			var err error
			if data, err = base64.StdEncoding.DecodeString(encoded); err != nil {
				return nil, archiveError("publication material has invalid originals")
			}
		}
		sum := sha256.Sum256(data)
		list = append(list, originalSummary{name: name, bytes: len(data), sha256: hex.EncodeToString(sum[:])})
	}
	return list, nil
}

// originals prints the summaries as an array, not a map: two files whose names
// are the same once cleaned each keep an entry.
func (m *materialText) originals(files []originalSummary) any {
	if files == nil {
		return nil
	}
	list := make([]any, len(files))
	for i, file := range files {
		list[i] = map[string]any{"name": m.clean(file.name), "bytes": file.bytes, "sha256": file.sha256}
	}
	return list
}

// value cleans one decoded JSON value. A string longer than maxMaterialText
// prints its first characters with its length and digest.
func (m *materialText) value(value any) any {
	switch v := value.(type) {
	case string:
		length := utf8.RuneCountInString(v)
		if length <= maxMaterialText {
			return m.clean(v)
		}
		sum := sha256.Sum256([]byte(v))
		return map[string]any{"text": m.cleanPrefix(v, maxMaterialText), "truncated": true, "length": length, "sha256": hex.EncodeToString(sum[:])}
	case []any:
		list := make([]any, len(v))
		for i, item := range v {
			list[i] = m.value(item)
		}
		return list
	case map[string]any:
		return m.object(v)
	}
	return value
}

// object cleans the keys of an object too. Two keys that are equal once cleaned
// stay apart: the later one gets a numbered suffix.
func (m *materialText) object(object map[string]any) map[string]any {
	cleaned := make(map[string]any, len(object))
	for _, key := range slices.Sorted(maps.Keys(object)) {
		name := m.clean(key)
		for n := 2; ; n++ {
			if _, taken := cleaned[name]; !taken {
				break
			}
			name = fmt.Sprintf("%s#%d", m.clean(key), n)
		}
		cleaned[name] = m.value(object[key])
	}
	return cleaned
}
