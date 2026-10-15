package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
)

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

// discoverNewCodexSession polls until exactly one rollout created at or after
// launchStart is bound to this task and launch directory, for at most wait.
func discoverNewCodexSession(taskID string, launchStart time.Time, cwd string, wait time.Duration) (string, error) {
	root, err := filepath.Abs(codexSessionsRoot())
	if err != nil {
		return "", err
	}
	scan := newCodexRolloutScan(root, codexPromptPrefixes(taskID), launchStart, cwd)
	deadline := nowFn().Add(wait)
	var last error
	for {
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			return "", last
		}
		ctx, cancel := probe.TimeoutContext(remaining)
		candidates, err := scan.candidates(ctx, nowFn())
		cancel()
		switch {
		case err != nil:
			last = err
		case len(candidates) == 1:
			return candidates[0], nil
		case len(candidates) > 1:
			return "", launchError(
				"launch.multiple_new_codex_sessions_appeared_during_launch", strconv.Itoa(len(candidates)),
			)
		default:
			last = launchError("launch.the_newly_started_codex_session_has_not_appeared_yet")
		}
		if !nowFn().Before(deadline) {
			return "", last
		}
		sleepFn(notifyPollInterval)
	}
}
