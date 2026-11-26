package launch

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/probe"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/terminal/direct"
)

// discoveredSessionProvider runs the declared session.discovery command: the
// ids it lists before the launch are the snapshot, and the one id that is new
// afterwards is the launch's session.
type discoveredSessionProvider struct{}

func init() { registerSessionProvider(discoveredSessionProvider{}) }

func (discoveredSessionProvider) Mode() string    { return "discovered" }
func (discoveredSessionProvider) Resumable() bool { return true }
func (discoveredSessionProvider) Persists() bool  { return true }

func (discoveredSessionProvider) Failures() discoveryFailures {
	return discoveryFailures{
		None:    "launch.the_newly_started_session_has_not_appeared_yet",
		Several: "launch.multiple_new_sessions_appeared_during_launch",
	}
}

func (discoveredSessionProvider) Wait(l sessionLaunch) time.Duration {
	return l.budget(l.Definition.Discovery.Timeout())
}

func (discoveredSessionProvider) Interval(l sessionLaunch) time.Duration {
	return l.Definition.Discovery.Interval()
}

func (discoveredSessionProvider) Snapshot(l sessionLaunch) (map[string]struct{}, error) {
	disc := l.Definition.Discovery
	ctx, cancel := probe.TimeoutContext(l.budget(disc.Timeout()))
	sessions, err := enumerateSessionsFn(ctx, disc, l.Program, l.CWD, l.TaskID)
	cancel()
	if err != nil {
		return nil, err
	}
	previous := make(map[string]struct{}, len(sessions))
	for _, id := range sessions {
		previous[id] = struct{}{}
	}
	return previous, nil
}

func (discoveredSessionProvider) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	return discoveredScan{launch: l, previous: previous}, nil
}

// discoveredScan lists the enumerated sessions that are new since the snapshot.
type discoveredScan struct {
	launch   sessionLaunch
	previous map[string]struct{}
}

func (s discoveredScan) Candidates(ctx context.Context) ([]string, error) {
	l := s.launch
	listed, err := enumerateSessionsFn(ctx, l.Definition.Discovery, l.Program, l.CWD, l.TaskID)
	if err != nil {
		return nil, err
	}
	var fresh []string
	for _, id := range listed {
		if _, ok := s.previous[id]; !ok {
			fresh = append(fresh, id)
		}
	}
	return fresh, nil
}

// enumerateSessionsFn runs the declared enumerate command and returns the
// validated session ids that satisfy every declared match. It is a var so
// tests can stub the agent CLI boundary.
var enumerateSessionsFn = enumerateAgentSessions

// enumerateAgentSessions runs the declared enumerate command directly (no
// shell) in the launch project directory and returns the validated ids of the
// records that satisfy every declared match.
func enumerateAgentSessions(ctx context.Context, disc *config.SessionDiscovery, program *process.AgentProgram, cwd, taskID string) ([]string, error) {
	if program == nil {
		return nil, launchError("launch.session_list_failed", "agent program is unavailable")
	}
	inv, err := launchInvocation(LaunchPlan{Launcher: direct.Foreground}, *program, disc.Args)
	if err != nil {
		return nil, launchError("launch.session_list_failed", err.Error())
	}
	result, err := probe.CaptureWithEnvDirLimit(ctx, inv.Argv[0], inv.Argv[1:], envSlice(inv.Env), cwd, disc.OutputLimit())
	if err != nil {
		return nil, launchError("launch.session_list_failed", err.Error())
	}
	if result.Overflow {
		return nil, launchError("launch.session_list_output_limit")
	}
	if result.Code != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			detail = "exit " + strconv.Itoa(result.Code)
		}
		return nil, launchError("launch.session_list_failed", detail)
	}
	return parseSessionList(disc, result.Stdout, cwd, taskID)
}

// canonicalSessionDir normalizes a directory for the {cwd} match on
// untrusted CLI output: both sides are cleaned and symlink-resolved so a
// different spelling of the same directory still binds.
func canonicalSessionDir(p string) string {
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}

func discoveryMatchEquals(template, cwd, taskID string) string {
	return strings.NewReplacer("{cwd}", canonicalSessionDir(cwd), "{task_id}", taskID).Replace(template)
}

// recordMatches reports whether one enumerated record satisfies every declared
// match. A missing or non-string field never matches.
func recordMatches(match []config.DiscoveryMatch, record map[string]any, cwd, taskID string) bool {
	for _, m := range match {
		value, ok := record[m.Field].(string)
		if !ok {
			return false
		}
		want := m.Equals
		if strings.Contains(want, "{cwd}") {
			if canonicalSessionDir(value) != discoveryMatchEquals(want, cwd, taskID) {
				return false
			}
			continue
		}
		if value != discoveryMatchEquals(want, cwd, taskID) {
			return false
		}
	}
	return true
}

// parseSessionList decodes one enumerate output into validated session ids.
// Malformed output, a missing id field, and an invalid id all fail closed.
func parseSessionList(disc *config.SessionDiscovery, stdout, cwd, taskID string) ([]string, error) {
	var records []map[string]any
	switch disc.Format {
	case "json":
		if err := json.Unmarshal([]byte(stdout), &records); err != nil {
			return nil, launchError("launch.session_list_invalid")
		}
	case "jsonl":
		for _, line := range strings.Split(stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil || record == nil {
				return nil, launchError("launch.session_list_invalid")
			}
			records = append(records, record)
		}
	case "lines":
		seen := map[string]struct{}{}
		var sessions []string
		for _, line := range strings.Split(stdout, "\n") {
			id := strings.TrimSpace(line)
			if id == "" {
				continue
			}
			if !sessionReferenceRe.MatchString(id) {
				return nil, launchError("launch.session_list_invalid")
			}
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				sessions = append(sessions, id)
			}
		}
		return sessions, nil
	}
	if disc.Format == "json" && records == nil {
		return nil, launchError("launch.session_list_invalid")
	}
	seen := map[string]struct{}{}
	var sessions []string
	for _, record := range records {
		if !recordMatches(disc.Match, record, cwd, taskID) {
			continue
		}
		id, ok := record[disc.IDField].(string)
		if !ok || !sessionReferenceRe.MatchString(id) {
			return nil, launchError("launch.session_list_invalid")
		}
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			sessions = append(sessions, id)
		}
	}
	return sessions, nil
}
