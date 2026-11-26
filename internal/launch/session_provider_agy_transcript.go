package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/probe"
)

const (
	// agyDiscoverWait is the discovery budget of one Antigravity launch. agy
	// needs a few seconds before it writes the transcript of a new conversation.
	agyDiscoverWait = 30 * time.Second
	// agyTranscriptHeaderLimit bounds the first transcript line read from one
	// conversation; a longer first line is never a launch instruction.
	agyTranscriptHeaderLimit = 256 << 10

	agyUserRequestOpen  = "<USER_REQUEST>\n"
	agyUserRequestClose = "\n</USER_REQUEST>"
)

// agyTranscriptSessionProvider finds the conversation Antigravity created for a
// launch. agy keeps one directory per conversation below its brain directory,
// named by the conversation id; the first line of its transcript is the first
// user request. That request carries the task file path of the launch, which is
// unique, so it names the conversation of this launch and no other, even while
// other agy sessions start in the same workspace.
type agyTranscriptSessionProvider struct{}

func init() { registerSessionProvider(agyTranscriptSessionProvider{}) }

func (agyTranscriptSessionProvider) Mode() string    { return "hook:agy-transcript" }
func (agyTranscriptSessionProvider) Resumable() bool { return true }
func (agyTranscriptSessionProvider) Persists() bool  { return true }

func (agyTranscriptSessionProvider) Failures() discoveryFailures {
	return discoveryFailures{
		None:    "launch.the_newly_started_session_has_not_appeared_yet",
		Several: "launch.multiple_new_sessions_appeared_during_launch",
	}
}

func (agyTranscriptSessionProvider) Wait(l sessionLaunch) time.Duration {
	return l.budget(agyDiscoverWait)
}

func (agyTranscriptSessionProvider) Interval(sessionLaunch) time.Duration {
	return notifyPollInterval
}

func (agyTranscriptSessionProvider) Snapshot(l sessionLaunch) (map[string]struct{}, error) {
	root, err := agyBrainRoot()
	if err != nil {
		return nil, launchError("launch.session_list_failed", err.Error())
	}
	ctx, cancel := probe.TimeoutContext(l.budget(sessionDiscoverWait))
	defer cancel()
	ids, err := agyConversationIDs(ctx, root)
	if err != nil {
		return nil, launchError("launch.session_list_failed", err.Error())
	}
	previous := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		previous[id] = struct{}{}
	}
	return previous, nil
}

// Scan reads only conversations absent from previous; several conversations
// that answer the launch instruction fail at once instead of guessing.
func (agyTranscriptSessionProvider) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	root, err := agyBrainRoot()
	if err != nil {
		return nil, err
	}
	return &agyTranscriptScan{root: root, previous: previous, prompt: l.Prompt, settled: map[string]struct{}{}}, nil
}

// agyBrainRoot is where agy keeps one directory per conversation. agy has no
// documented way to move it, so the location is fixed below the user home.
func agyBrainRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(home, ".gemini", "antigravity-cli", "brain"))
}

// agyConversationIDs lists the conversation directories below root. A missing
// root is an agy that never ran, so it has none. A symlink is not a directory
// entry here and is never listed.
func agyConversationIDs(ctx context.Context, root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() && sessionReferenceRe.MatchString(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}

// agyFirstRequestMatches reports whether the first transcript line of the
// conversation id is the user request prompt. The transcript is data written
// by agy and is untrusted: it is opened without following links, read up to a
// fixed size, parsed as JSON and compared as a string, nothing more. retry is
// true while the file or its first line does not exist yet, so a later poll
// reads it again.
func agyFirstRequestMatches(root, id, prompt string) (match, retry bool, err error) {
	path := filepath.Join(root, id, ".system_generated", "logs", "transcript.jsonl")
	file, err := fs.OpenRegularFileIfExists(root, path)
	if err != nil {
		return false, false, err
	}
	if file == nil {
		return false, true, nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, agyTranscriptHeaderLimit))
	if err != nil {
		return false, false, err
	}
	line, _, complete := bytes.Cut(data, []byte("\n"))
	if !complete {
		return false, len(data) < agyTranscriptHeaderLimit, nil
	}
	var step struct {
		Source  string `json:"source"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if json.Unmarshal(line, &step) != nil || step.Source != "USER_EXPLICIT" || step.Type != "USER_INPUT" {
		return false, false, nil
	}
	body, ok := strings.CutPrefix(step.Content, agyUserRequestOpen)
	if !ok {
		return false, false, nil
	}
	request, _, closed := strings.Cut(body, agyUserRequestClose)
	return closed && request == prompt, false, nil
}

// agyTranscriptScan finds the conversations one launch created. It opens only
// conversations absent from the pre-launch snapshot and remembers every one that
// can never match, so later polls do not open it again. A transcript that cannot
// be read does not stop the scan: its error is returned only when no
// conversation matches.
type agyTranscriptScan struct {
	root     string
	previous map[string]struct{}
	prompt   string
	settled  map[string]struct{}
}

func (s *agyTranscriptScan) Candidates(ctx context.Context) ([]string, error) {
	// Without a launch instruction nothing can be told apart from other agy
	// sessions, so nothing matches.
	if s.prompt == "" {
		return nil, nil
	}
	ids, err := agyConversationIDs(ctx, s.root)
	if err != nil {
		return nil, err
	}
	var sessions []string
	var readErr error
	for _, id := range ids {
		if _, ok := s.previous[id]; ok {
			continue
		}
		if _, ok := s.settled[id]; ok {
			continue
		}
		match, retry, err := agyFirstRequestMatches(s.root, id, s.prompt)
		if err != nil {
			readErr = err
			continue
		}
		if match {
			sessions = append(sessions, id)
		} else if !retry {
			s.settled[id] = struct{}{}
		}
	}
	if len(sessions) == 0 && readErr != nil {
		return nil, readErr
	}
	slices.Sort(sessions)
	return sessions, nil
}
