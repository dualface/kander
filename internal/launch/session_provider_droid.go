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

// droidSessionHeaderLimit bounds the session_start line read from one Droid
// session file; a longer first line is never a session header.
const droidSessionHeaderLimit = 64 << 10

// droidSessionProvider finds the session Droid created for a launch: a new
// <id>.jsonl below ~/.factory/sessions whose session_start header names the
// launch directory.
type droidSessionProvider struct{}

func init() { registerSessionProvider(droidSessionProvider{}) }

func (droidSessionProvider) Mode() string    { return "hook:droid-sessions" }
func (droidSessionProvider) Resumable() bool { return true }
func (droidSessionProvider) Persists() bool  { return true }

func (droidSessionProvider) Failures() discoveryFailures {
	return discoveryFailures{
		None:    "launch.the_newly_started_session_has_not_appeared_yet",
		Several: "launch.multiple_new_sessions_appeared_during_launch",
	}
}

func (droidSessionProvider) Wait(l sessionLaunch) time.Duration {
	return l.budget(sessionDiscoverWait)
}

func (droidSessionProvider) Interval(sessionLaunch) time.Duration { return notifyPollInterval }

func (droidSessionProvider) Snapshot(l sessionLaunch) (map[string]struct{}, error) {
	return droidSessionSnapshot(l.budget(sessionDiscoverWait))
}

// Scan reads only sessions absent from previous; several new sessions for the
// launch directory fail at once instead of guessing.
func (droidSessionProvider) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	root, err := filepath.Abs(droidSessionsRoot())
	if err != nil {
		return nil, err
	}
	return &droidSessionScan{root: root, previous: previous, cwd: l.CWD, settled: map[string]struct{}{}}, nil
}

// droidSessionsRoot is where Droid keeps one directory per launch directory and
// one <id>.jsonl file per session inside it.
func droidSessionsRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".factory", "sessions")
}

// droidSessionFiles maps every session id below root to its file. Only the
// file names are read: the id is the name without .jsonl.
func droidSessionFiles(ctx context.Context, root string) (map[string]string, error) {
	files := map[string]string{}
	dirs, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return files, nil
	}
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, dir.Name()))
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
			id, ok := strings.CutSuffix(entry.Name(), ".jsonl")
			if !ok || !entry.Type().IsRegular() || !sessionReferenceRe.MatchString(id) {
				continue
			}
			files[id] = filepath.Join(root, dir.Name(), entry.Name())
		}
	}
	return files, nil
}

// droidSessionSnapshot records the session ids that exist before a launch.
func droidSessionSnapshot(budget time.Duration) (map[string]struct{}, error) {
	root, err := filepath.Abs(droidSessionsRoot())
	if err != nil {
		return nil, err
	}
	ctx, cancel := probe.TimeoutContext(budget)
	defer cancel()
	files, err := droidSessionFiles(ctx, root)
	if err != nil {
		return nil, launchError("launch.session_list_failed", err.Error())
	}
	previous := make(map[string]struct{}, len(files))
	for id := range files {
		previous[id] = struct{}{}
	}
	return previous, nil
}

// readDroidSessionHeader reports whether the file at path is the session id's
// session_start record for cwd. retry is true while the header line is still
// being written, so a later poll reads the file again.
func readDroidSessionHeader(root, path, id, cwd string) (match, retry bool, err error) {
	file, err := fs.OpenRegularFileIfExists(root, path)
	if err != nil {
		return false, false, err
	}
	if file == nil {
		return false, true, nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, droidSessionHeaderLimit))
	if err != nil {
		return false, false, err
	}
	line, _, complete := bytes.Cut(data, []byte("\n"))
	if !complete {
		return false, len(data) < droidSessionHeaderLimit, nil
	}
	var header struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		CWD  string `json:"cwd"`
	}
	if json.Unmarshal(line, &header) != nil || header.Type != "session_start" || header.ID != id {
		return false, false, nil
	}
	return matchesSessionDirectory(header.CWD, cwd), false, nil
}

// droidSessionScan finds the sessions one launch created. It opens only files
// absent from the pre-launch snapshot and remembers every file that can never
// match, so later polls do not open it again. A file that cannot be read does
// not stop the scan: its error is returned only when no session matches.
type droidSessionScan struct {
	root     string
	previous map[string]struct{}
	cwd      string
	settled  map[string]struct{}
}

func (s *droidSessionScan) Candidates(ctx context.Context) ([]string, error) {
	files, err := droidSessionFiles(ctx, s.root)
	if err != nil {
		return nil, err
	}
	var sessions []string
	var readErr error
	for id, path := range files {
		if _, ok := s.previous[id]; ok {
			continue
		}
		if _, ok := s.settled[id]; ok {
			continue
		}
		match, retry, err := readDroidSessionHeader(s.root, path, id, s.cwd)
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
