package launch

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/process"
)

// A session provider owns how one session mechanism names the session of an
// agent launch: where its id comes from before the launch and how the launch's
// own session is found afterwards. Each mechanism lives in one
// session_provider_<mechanism>.go file and registers itself by its
// session.mode value. Callers go through the entry points below and never
// compare mode names; adding a mechanism means adding a provider file and
// registering it (config only lists the hook names and their capability flags,
// and the consistency test keeps both sides in step).
//
// This branch removed local resume, adoption and session repair, so the
// resolver, adopter and prover capabilities of the develop branch (finding a
// card's session again, listing adoption candidates, proving which session a
// running process holds open) are not ported: nothing here would call them.
// The two judgments the remaining callers need are optional capabilities
// instead, sessionAllocatorChecker and sessionMarkerRepairer.

// sessionLaunch carries the facts a provider may use for one launch.
type sessionLaunch struct {
	TaskID string
	// Prompt is the instruction this launch sends to the agent. It is empty
	// before the instruction exists, for example while the pre-launch snapshot
	// is taken.
	Prompt string
	CWD    string
	// Start is the time recorded right before the agent was launched.
	Start      time.Time
	Program    *process.AgentProgram
	Definition *config.AgentSessionDefinition
	// Budget maps one launch-time wait to its budget (LaunchPlan.launchTimeout).
	Budget func(time.Duration) time.Duration
}

// budget applies the launch budget; a launch without one keeps every wait.
func (l sessionLaunch) budget(wait time.Duration) time.Duration {
	if l.Budget == nil {
		return wait
	}
	return l.Budget(wait)
}

// sessionProvider is the part every mechanism has.
type sessionProvider interface {
	// Mode is the session.mode value the provider serves, such as "generated"
	// or "hook:<name>" for a hook.
	Mode() string
	// Resumable reports whether the agent keeps a session a later launch can
	// resume. A provider that is not resumable gets no session id in its
	// launch arguments and refuses resume.
	Resumable() bool
}

// sessionAllocator obtains the session id before the agent starts. A provider
// without it leaves the reference empty until discovery or resolution.
type sessionAllocator interface {
	sessionProvider
	Allocate(l sessionLaunch) (string, error)
}

// discoveryFailures are the localized error keys of one provider's discovery.
type discoveryFailures struct {
	// None is the key reported while no new session has appeared.
	None string
	// Several is the key reported, with the candidate count, when more than one
	// new session appeared.
	Several string
}

// sessionScan lists the candidate sessions of one launch; it selects them
// itself, so the shared loop only counts.
type sessionScan interface {
	Candidates(ctx context.Context) ([]string, error)
}

// sessionDiscoverer finds the session a launch created.
type sessionDiscoverer interface {
	sessionProvider
	// Snapshot records the sessions that exist before the launch.
	Snapshot(l sessionLaunch) (map[string]struct{}, error)
	// Scan opens a scan of the launch's candidates. previous is the snapshot.
	Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error)
	// Wait is the total discovery budget and Interval the pause between polls.
	Wait(l sessionLaunch) time.Duration
	Interval(l sessionLaunch) time.Duration
	Failures() discoveryFailures
	// Persists reports whether the discovered id replaces the provisional
	// session value of the task card on every launcher; a remote start also
	// replaces it on a launcher that marks the pane or reports the session.
	Persists() bool
}

// sessionAllocatorChecker is an allocator that can tell, without running
// anything, whether its allocation can start. A caller that records the
// allocation as an external effect checks first, so a missing allocator stays a
// plain, retryable refusal. A provider without it always passes.
type sessionAllocatorChecker interface {
	sessionAllocator
	RequireAllocator(l sessionLaunch) error
}

// sessionMarkerRepairer marks a provider whose launch sessions can be found
// again from static facts, so the session marker a launch left out can be
// written later. A provider without it cannot be repaired unless the card
// already names the session.
type sessionMarkerRepairer interface {
	sessionProvider
	MarkerRepairable() bool
}

var sessionProviders = map[string]sessionProvider{}

// registerSessionProvider adds a provider by its mode. Providers register
// from init, so a duplicate is a programming error.
func registerSessionProvider(p sessionProvider) {
	if _, dup := sessionProviders[p.Mode()]; dup {
		panic(fmt.Sprintf("launch: session provider %q registered twice", p.Mode()))
	}
	sessionProviders[p.Mode()] = p
}

// sessionProviderFor is the only place that looks a provider up by session
// mode. An agent the configuration marked unusable has no provider even when
// its built-in namesake would supply a session definition, and a definition no
// provider serves is one this version cannot use; both are reported like any
// unavailable agent.
func sessionProviderFor(cfg *config.Config, agent string, def *config.AgentSessionDefinition) (sessionProvider, error) {
	if cfg != nil {
		if _, unusable := cfg.UnusableAgents[agent]; unusable {
			return nil, unavailableAgentError(cfg, agent)
		}
	}
	if def != nil {
		if p, ok := sessionProviders[def.Mode]; ok {
			return p, nil
		}
	}
	return nil, unavailableAgentError(cfg, agent)
}

// sessionProviderOf is sessionProviderFor for the effective definition of agent.
func sessionProviderOf(cfg *config.Config, agent string) (sessionProvider, error) {
	return sessionProviderFor(cfg, agent, config.AgentFor(cfg, agent).Session)
}

// allocateSession obtains the session id before launch, or returns "" when the
// provider has no id to give before the agent starts.
func allocateSession(p sessionProvider, l sessionLaunch) (string, error) {
	if allocator, ok := p.(sessionAllocator); ok {
		return allocator.Allocate(l)
	}
	return "", nil
}

// sessionDiscovers reports whether launch must identify the new session after
// start.
func sessionDiscovers(p sessionProvider) bool {
	_, ok := p.(sessionDiscoverer)
	return ok
}

// sessionPersists reports whether a discovered id must replace the provisional
// task-card session value on every launcher. A remote start also replaces it on
// a launcher that marks the pane or reports the session.
func sessionPersists(p sessionProvider) bool {
	discoverer, ok := p.(sessionDiscoverer)
	return ok && discoverer.Persists()
}

// snapshotSessions records the sessions that exist before a launch whose new
// session is discovered afterwards. discover is false when this launch will
// not look for one; the snapshot is then empty.
func snapshotSessions(p sessionProvider, l sessionLaunch, discover bool) (map[string]struct{}, error) {
	discoverer, ok := p.(sessionDiscoverer)
	if !ok || !discover {
		return map[string]struct{}{}, nil
	}
	return discoverer.Snapshot(l)
}

// discoverLaunchSession finds the session a launch created. previous is the
// snapshot taken before it.
func discoverLaunchSession(p sessionProvider, l sessionLaunch, previous map[string]struct{}) (string, error) {
	discoverer, ok := p.(sessionDiscoverer)
	if !ok {
		return "", launchError("launch.unsupported_agent", p.Mode())
	}
	scan, err := discoverer.Scan(l, previous)
	if err != nil {
		return "", err
	}
	return waitForSessionCandidates(scan, discoverer.Wait(l), discoverer.Interval(l), discoverer.Failures())
}

// requireSessionAllocator reports why the provider's allocation cannot start,
// or nil when it can or the provider has no allocation to check.
func requireSessionAllocator(p sessionProvider, l sessionLaunch) error {
	if checker, ok := p.(sessionAllocatorChecker); ok {
		return checker.RequireAllocator(l)
	}
	return nil
}

// sessionMarkerRepairable reports whether the session marker a launch left out
// can be written for this provider without the card naming the session.
func sessionMarkerRepairable(p sessionProvider) bool {
	repairer, ok := p.(sessionMarkerRepairer)
	return ok && repairer.MarkerRepairable()
}

// waitForSessionCandidates is the one polling loop of session discovery. It
// scans until exactly one candidate appears, for at most wait: a single
// candidate returns at once, several fail at once, and none (or a scan that
// fails) is retried after interval, keeping the last error for the end.
func waitForSessionCandidates(scan sessionScan, wait, interval time.Duration, failures discoveryFailures) (string, error) {
	deadline := nowFn().Add(wait)
	var last error
	for {
		remaining := deadline.Sub(nowFn())
		if remaining <= 0 {
			if last == nil {
				last = launchError(failures.None)
			}
			return "", last
		}
		ctx, cancel := probe.TimeoutContext(remaining)
		candidates, err := scan.Candidates(ctx)
		cancel()
		switch {
		case err != nil:
			last = err
		case len(candidates) == 1:
			return candidates[0], nil
		case len(candidates) > 1:
			return "", launchError(failures.Several, strconv.Itoa(len(candidates)))
		default:
			last = launchError(failures.None)
		}
		if !nowFn().Before(deadline) {
			return "", last
		}
		sleepFn(interval)
	}
}
