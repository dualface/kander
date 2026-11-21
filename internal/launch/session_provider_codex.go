package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

// codexSessionProvider finds the Codex session a launch created. Codex writes
// one rollout file per session below $CODEX_HOME/sessions; a rollout names its
// session, its launch directory and its creation time, and its first user
// message starts with this task's prompt. The same facts let a later adoption
// find the session again (MarkerRepairable).
type codexSessionProvider struct{}

func init() { registerSessionProvider(codexSessionProvider{}) }

func (codexSessionProvider) Mode() string    { return "hook:codex-rollout" }
func (codexSessionProvider) Resumable() bool { return true }
func (codexSessionProvider) Persists() bool  { return false }

func (codexSessionProvider) Failures() discoveryFailures {
	return discoveryFailures{
		None:    "launch.the_newly_started_codex_session_has_not_appeared_yet",
		Several: "launch.multiple_new_codex_sessions_appeared_during_launch",
	}
}

// Wait is a fixed ten seconds that the launch budget can only raise, so the
// loop always makes a first scan; the old Codex loop alone returned an empty id
// without an error for a wait of zero or less, which no launch reaches.
func (codexSessionProvider) Wait(l sessionLaunch) time.Duration {
	return l.budget(sessionDiscoverWait)
}

func (codexSessionProvider) Interval(sessionLaunch) time.Duration { return notifyPollInterval }

// Snapshot reads no rollout: discovery selects new sessions by creation time,
// so only a sessions root that cannot be one fails early.
func (codexSessionProvider) Snapshot(sessionLaunch) (map[string]struct{}, error) {
	if err := checkCodexSessionsRoot(); err != nil {
		return nil, err
	}
	return map[string]struct{}{}, nil
}

// Scan selects the rollouts created at or after the launch whose header names
// the launch directory and whose user prompt carries this task's prompt prefix.
func (codexSessionProvider) Scan(l sessionLaunch, _ map[string]struct{}) (sessionScan, error) {
	root, err := filepath.Abs(codexSessionsRoot())
	if err != nil {
		return nil, err
	}
	return codexScanner{newCodexRolloutScan(root, codexPromptPrefixes(l.TaskID), l.Start, l.CWD)}, nil
}

// codexScanner reads the clock for the shared loop; the rollout scan itself
// takes the time as an argument so tests can fix it.
type codexScanner struct{ *codexRolloutScan }

func (s codexScanner) Candidates(ctx context.Context) ([]string, error) {
	return s.candidates(ctx, nowFn())
}

// MarkerRepairable: a rollout names its task, its launch directory and its
// creation time, so the launch's session can be found again.
func (codexSessionProvider) MarkerRepairable() bool { return true }

// codexRolloutNameSkew bounds how far the wall-clock time in a rollout file
// name can sit from UTC. Codex names rollouts and their date directories in its
// own process time zone, which may differ from Kander's; UTC offsets span -12h
// to +14h, so a file named more than 26 hours before the launch can never hold
// a session created after it.
const codexRolloutNameSkew = 26 * time.Hour

// codexRolloutNameLayout is the wall-clock time Codex embeds in rollout file
// names: rollout-2006-01-02T15-04-05-<id>.jsonl.
const codexRolloutNameLayout = "2006-01-02T15-04-05"

// checkCodexSessionsRoot fails when the Codex sessions root exists but is not a
// directory. A missing root is fine: Codex creates it on its first session.
func checkCodexSessionsRoot() error {
	root := codexSessionsRoot()
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return launchError("launch.codex_sessions_directory_not_found_cannot_resume_with_context", root)
	}
	return nil
}

// codexCandidateDays lists the YYYY/MM/DD rollout directories that can hold a
// session created between launchStart and now: one UTC day of margin on each
// side covers any time zone Codex may name its directories in.
func codexCandidateDays(launchStart, now time.Time) []string {
	first := launchStart.UTC().AddDate(0, 0, -1)
	last := now.UTC().AddDate(0, 0, 1)
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	var days []string
	for !day.After(last) {
		days = append(days, filepath.FromSlash(day.Format("2006/01/02")))
		day = day.AddDate(0, 0, 1)
	}
	return days
}

// codexRolloutNameTime parses the wall-clock time of a rollout file name, read
// as UTC because the writer's time zone is unknown.
func codexRolloutNameTime(base string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(base, "rollout-")
	if !ok || len(rest) < len(codexRolloutNameLayout) {
		return time.Time{}, false
	}
	named, err := time.Parse(codexRolloutNameLayout, rest[:len(codexRolloutNameLayout)])
	return named, err == nil
}

// codexRolloutScan finds the rollouts created by one launch. It reads only the
// candidate date directories and remembers every file that can never become a
// candidate, so later polls do not open it again.
type codexRolloutScan struct {
	root        string
	prefixes    []string
	launchStart time.Time
	cwd         string
	settled     map[string]struct{}
	// opened counts rollout files opened, for tests.
	opened int
}

func newCodexRolloutScan(root string, prefixes []string, launchStart time.Time, cwd string) *codexRolloutScan {
	return &codexRolloutScan{
		root:        root,
		prefixes:    prefixes,
		launchStart: launchStart.Truncate(time.Millisecond),
		cwd:         cwd,
		settled:     map[string]struct{}{},
	}
}

// candidates returns the ids of rollouts created at or after the launch whose
// header names the launch directory and whose user prompt carries this task's
// prompt prefix.
func (s *codexRolloutScan) candidates(ctx context.Context, now time.Time) ([]string, error) {
	var sessions []string
	seen := map[string]struct{}{}
	for _, day := range codexCandidateDays(s.launchStart, now) {
		entries, err := os.ReadDir(filepath.Join(s.root, day))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id, err := s.check(ctx, filepath.Join(s.root, day, entry.Name()), entry)
			if err != nil {
				return nil, err
			}
			if id == "" {
				continue
			}
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				sessions = append(sessions, id)
			}
		}
	}
	return sessions, nil
}

// check returns the session id when the rollout at path is a candidate. A file
// that is still being written is retried on the next poll; every other
// rejection is settled.
func (s *codexRolloutScan) check(ctx context.Context, path string, entry os.DirEntry) (string, error) {
	if _, ok := s.settled[path]; ok {
		return "", nil
	}
	base := entry.Name()
	if !entry.Type().IsRegular() || !strings.HasPrefix(base, "rollout-") || !strings.HasSuffix(base, ".jsonl") {
		s.settled[path] = struct{}{}
		return "", nil
	}
	if named, ok := codexRolloutNameTime(base); ok && named.Before(s.launchStart.Add(-codexRolloutNameSkew)) {
		s.settled[path] = struct{}{}
		return "", nil
	}
	info, err := entry.Info()
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	s.opened++
	session, err := readRolloutSession(ctx, s.root, process.OpenFile{Path: path, Info: info}, s.prefixes, s.cwd)
	if err != nil {
		var mismatch *sessionDirectoryMismatch
		if errors.As(err, &mismatch) {
			s.settled[path] = struct{}{}
			return "", nil
		}
		return "", err
	}
	if session.id == "" {
		return "", nil
	}
	if session.created.IsZero() || session.created.Before(s.launchStart) {
		s.settled[path] = struct{}{}
		return "", nil
	}
	if !session.bound {
		return "", nil
	}
	return session.id, nil
}

func codexSessionsRoot() string {
	home := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, "sessions")
}

// legacyPromptHeads are the localized heads earlier releases wrote into Codex rollouts; the
// current head comes from the English prompt template, but sessions created before the
// templates existed still start with one of these.
var legacyPromptHeads = map[string][]string{
	"start":    {"执行 Kanban 任务 %s", "Kanban タスク %s を実行します"},
	"resume":   {"继续 Kanban 任务 %s", "Kanban タスク %s を再開します"},
	"takeover": {"接管 Kanban 任务 %s", "Kanban タスク %s を引き継ぎます"},
}

// promptPrefixes lists every head a rollout of the given kinds may start with: the current
// template head, rendered from the same installed template the launch wrote with, and the
// legacy localized heads. A head that fails to render leaves only the legacy heads.
func promptPrefixes(taskID string, kinds ...string) []string {
	paths, err := currentInstallPaths()
	if err != nil {
		paths = config.InstallPaths{}
	}
	var out []string
	for _, kind := range kinds {
		var heads []string
		if head, err := promptHead(paths, kind, taskID, "", 0); err == nil {
			heads = append(heads, strings.TrimSuffix(head, "."))
		}
		for _, legacy := range legacyPromptHeads[kind] {
			heads = append(heads, fmt.Sprintf(legacy, taskID))
		}
		for _, head := range heads {
			out = append(out, head+";", head+".")
		}
	}
	return out
}

func takeoverPromptPrefixes(taskID string) []string {
	return promptPrefixes(taskID, "takeover")
}

func codexPromptPrefixes(taskID string) []string {
	return promptPrefixes(taskID, "start", "resume", "takeover")
}

func startsWithAny(text string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// rolloutSession is what one rollout file says about its session.
type rolloutSession struct {
	// id is empty when the session_meta header is missing, invalid, or not fully written yet.
	id string
	// created is the session creation time; zero when the header carries no valid timestamp.
	created time.Time
	// bound reports whether a user message begins with one of the task prompt prefixes.
	bound bool
}

// readRolloutSession reads at most 64 records / 1 MiB of an observed rollout.
// A header for another launch directory returns a *sessionDirectoryMismatch.
func readRolloutSession(ctx context.Context, root string, observed process.OpenFile, prefixes []string, cwd string) (rolloutSession, error) {
	file, err := fs.OpenRegularFileIfExists(root, observed.Path)
	if err != nil || file == nil {
		return rolloutSession{}, repairError("launch.session_repair_binding_missing")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || observed.Info == nil || !os.SameFile(info, observed.Info) {
		return rolloutSession{}, repairError("launch.session_repair_changed")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	var session rolloutSession
	for index := range 64 {
		if err := ctx.Err(); err != nil {
			return rolloutSession{}, err
		}
		var record struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID        string `json:"id"`
				CWD       string `json:"cwd"`
				Timestamp string `json:"timestamp"`
				Type      string `json:"type"`
				Role      string `json:"role"`
				Message   string `json:"message"`
				Content   []struct {
					Text string `json:"text"`
					Type string `json:"type"`
				} `json:"content"`
			} `json:"payload"`
		}
		if err := decoder.Decode(&record); err != nil {
			return session, nil
		}
		if index == 0 {
			if record.Type != "session_meta" || !sessionReferenceRe.MatchString(record.Payload.ID) {
				return rolloutSession{}, nil
			}
			if !matchesSessionDirectory(record.Payload.CWD, cwd) {
				return rolloutSession{}, &sessionDirectoryMismatch{repairError("launch.session_repair_directory_mismatch")}
			}
			session.id = record.Payload.ID
			session.created = rolloutCreatedAt(record.Payload.Timestamp, record.Timestamp)
			continue
		}
		if record.Type == "event_msg" && record.Payload.Type == "user_message" && startsWithAny(record.Payload.Message, prefixes) {
			session.bound = true
			return session, nil
		}
		if record.Type == "response_item" && record.Payload.Role == "user" {
			for _, content := range record.Payload.Content {
				if (content.Type == "input_text" || content.Type == "text") && startsWithAny(content.Text, prefixes) {
					session.bound = true
					return session, nil
				}
			}
		}
	}
	return session, nil
}

// rolloutCreatedAt parses the session creation time of a session_meta header.
// The payload timestamp is the creation time; the record timestamp is when the
// header was written and only stands in when the payload has none.
func rolloutCreatedAt(payload, record string) time.Time {
	for _, value := range []string{payload, record} {
		if value == "" {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}
		}
		return created
	}
	return time.Time{}
}
