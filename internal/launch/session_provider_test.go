package launch

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/config"
)

// scriptedScan returns one scripted result per poll and the last one after that.
type scriptedScan struct {
	results []scanResult
	polls   int
}

type scanResult struct {
	ids []string
	err error
}

func (s *scriptedScan) Candidates(context.Context) ([]string, error) {
	r := s.results[min(s.polls, len(s.results)-1)]
	s.polls++
	return r.ids, r.err
}

var testFailures = discoveryFailures{
	None:    "launch.the_newly_started_session_has_not_appeared_yet",
	Several: "launch.multiple_new_sessions_appeared_during_launch",
}

// recordSleeps replaces the clock with one that only moves when slept on.
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	previousNow, previousSleep := nowFn, sleepFn
	t.Cleanup(func() { nowFn, sleepFn = previousNow, previousSleep })
	clock := time.Unix(1_800_000_000, 0)
	var slept []time.Duration
	nowFn = func() time.Time { return clock }
	sleepFn = func(d time.Duration) {
		slept = append(slept, d)
		clock = clock.Add(d)
	}
	return &slept
}

func TestWaitForSessionCandidatesReturnsTheOnlyCandidateAtOnce(t *testing.T) {
	slept := recordSleeps(t)
	got, err := waitForSessionCandidates(&scriptedScan{results: []scanResult{{ids: []string{"only"}}}}, time.Minute, time.Second, testFailures)
	if err != nil || got != "only" || len(*slept) != 0 {
		t.Fatalf("got=%q err=%v slept=%v", got, err, *slept)
	}
}

func TestWaitForSessionCandidatesFailsAtOnceOnSeveralCandidates(t *testing.T) {
	slept := recordSleeps(t)
	_, err := waitForSessionCandidates(&scriptedScan{results: []scanResult{{ids: []string{"a", "b", "c"}}}}, time.Minute, time.Second, testFailures)
	want := launchError(testFailures.Several, "3").Error()
	if err == nil || err.Error() != want || len(*slept) != 0 {
		t.Fatalf("err=%v want=%q slept=%v", err, want, *slept)
	}
}

func TestWaitForSessionCandidatesRetriesUntilTheWaitEndsWithTheNoneError(t *testing.T) {
	slept := recordSleeps(t)
	scan := &scriptedScan{results: []scanResult{{}}}
	_, err := waitForSessionCandidates(scan, 3*time.Second, time.Second, testFailures)
	if want := launchError(testFailures.None).Error(); err == nil || err.Error() != want {
		t.Fatalf("err=%v want=%q", err, want)
	}
	if scan.polls != 3 || !slices.Equal(*slept, []time.Duration{time.Second, time.Second, time.Second}) {
		t.Fatalf("polls=%d slept=%v", scan.polls, *slept)
	}
}

func TestWaitForSessionCandidatesKeepsTheLastScanError(t *testing.T) {
	recordSleeps(t)
	boom := errors.New("scan failed")
	scan := &scriptedScan{results: []scanResult{{err: errors.New("earlier")}, {err: boom}}}
	_, err := waitForSessionCandidates(scan, 2*time.Second, time.Second, testFailures)
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want the last scan error", err)
	}
}

func TestWaitForSessionCandidatesFindsACandidateAfterRetries(t *testing.T) {
	slept := recordSleeps(t)
	scan := &scriptedScan{results: []scanResult{{}, {err: errors.New("transient")}, {ids: []string{"late"}}}}
	got, err := waitForSessionCandidates(scan, time.Minute, 250*time.Millisecond, testFailures)
	if err != nil || got != "late" || len(*slept) != 2 {
		t.Fatalf("got=%q err=%v slept=%v", got, err, *slept)
	}
}

func TestWaitForSessionCandidatesWithoutWaitReportsNone(t *testing.T) {
	recordSleeps(t)
	scan := &scriptedScan{results: []scanResult{{ids: []string{"never-read"}}}}
	_, err := waitForSessionCandidates(scan, 0, time.Second, testFailures)
	if want := launchError(testFailures.None).Error(); err == nil || err.Error() != want || scan.polls != 0 {
		t.Fatalf("err=%v polls=%d", err, scan.polls)
	}
}

// Every discovering provider uses the shared loop and so waits at least one
// poll: its wait is positive even before the launch budget raises it.
func TestSessionProvidersWaitAtLeastOnePoll(t *testing.T) {
	for mode, provider := range sessionProviders {
		discoverer, ok := provider.(sessionDiscoverer)
		if !ok {
			continue
		}
		l := sessionLaunch{Definition: &config.AgentSessionDefinition{Mode: mode, Discovery: devinDiscovery(t)}}
		if discoverer.Wait(l) <= 0 || discoverer.Interval(l) <= 0 {
			t.Errorf("%s: wait=%v interval=%v", mode, discoverer.Wait(l), discoverer.Interval(l))
		}
	}
}

// The hook list in internal/config and the providers here describe the same
// mechanisms: one provider per hook, with the capabilities the flags declare.
// ResolveEmptyReference is not compared: the resolver capability is not ported
// to this branch, and internal/liveness is the only consumer of that flag.
func TestSessionProvidersMatchTheRegisteredHooks(t *testing.T) {
	for _, hook := range config.RegisteredSessionHooks() {
		mode := "hook:" + hook.Name
		provider, ok := sessionProviders[mode]
		if !ok {
			t.Errorf("hook %q has no session provider", hook.Name)
			continue
		}
		def := &config.AgentSessionDefinition{Mode: mode}
		_, allocates := provider.(sessionAllocator)
		_, discovers := provider.(sessionDiscoverer)
		for _, check := range []struct {
			what     string
			provider bool
			flag     bool
		}{
			{"allocate before start", allocates, hook.AllocateBeforeStart},
			{"discover after start", discovers, hook.DiscoverAfterStart},
			{"persist after start", sessionPersists(provider), hook.PersistAfterStart},
			{"config allocate helper", allocates, config.SessionAllocatesBeforeStart(def)},
			{"config discover helper", discovers, config.SessionDiscoversAfterStart(def)},
			{"config persist helper", sessionPersists(provider), config.SessionPersistsAfterStart(def)},
		} {
			if check.provider != check.flag {
				t.Errorf("hook %q: %s is %v in the provider and %v in config", hook.Name, check.what, check.provider, check.flag)
			}
		}
	}
	for mode, provider := range sessionProviders {
		name, isHook := config.ParseSessionHook(mode)
		if _, registered := config.LookupSessionHook(mode); isHook && !registered {
			t.Errorf("provider %q is not registered in config", name)
		}
		if provider.Mode() != mode {
			t.Errorf("provider registered as %q reports mode %q", mode, provider.Mode())
		}
	}
	// discovered is a plain mode, not a hook, with the same discovery flags.
	def := &config.AgentSessionDefinition{Mode: "discovered"}
	if !config.SessionDiscoversAfterStart(def) || !sessionDiscovers(sessionProviders["discovered"]) ||
		!config.SessionPersistsAfterStart(def) || !sessionPersists(sessionProviders["discovered"]) {
		t.Error("discovered provider and config flags disagree")
	}
}

func TestOnlyTheNoneProviderIsNotResumable(t *testing.T) {
	for mode, provider := range sessionProviders {
		if provider.Resumable() != (mode != "none") {
			t.Errorf("%s: resumable=%v", mode, provider.Resumable())
		}
	}
}

// A session definition no provider serves is reported as an unavailable agent,
// never as an unsupported one.
func TestMissingSessionProviderReportsTheUnavailableAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents = map[string]config.AgentDefinition{}
	cfg.Agents["ghost"] = config.AgentDefinition{Session: &config.AgentSessionDefinition{Mode: "hook:from-a-newer-version"}}
	want := config.UnavailableAgentError(cfg, "ghost").Error()
	for name, call := range map[string]func() error{
		"new session": func() error { _, err := newAgentSession("ghost", nil, cfg); return err },
		"arguments": func() error {
			_, err := expandAgentInvocation("ghost", "", "", AgentSession{Agent: "ghost"}, false, cfg)
			return err
		},
	} {
		err := call()
		if err == nil || err.Error() != want || strings.Contains(err.Error(), "unsupported") {
			t.Errorf("%s: err=%v, want %q", name, err, want)
		}
	}
	cfg.UnusableAgents = map[string]string{"ghost": "session.mode is unknown"}
	if _, err := newAgentSession("ghost", nil, cfg); err == nil || err.Error() != config.UnavailableAgentError(cfg, "ghost").Error() {
		t.Errorf("unusable agent: err=%v", err)
	}
}

// promptProvider records the launch facts it is handed.
type promptProvider struct{ seen *sessionLaunch }

func (promptProvider) Mode() string    { return "test-prompt" }
func (promptProvider) Resumable() bool { return true }
func (promptProvider) Persists() bool  { return false }
func (promptProvider) Snapshot(sessionLaunch) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}
func (p promptProvider) Scan(l sessionLaunch, _ map[string]struct{}) (sessionScan, error) {
	*p.seen = l
	return &scriptedScan{results: []scanResult{{ids: []string{"seen"}}}}, nil
}
func (promptProvider) Wait(sessionLaunch) time.Duration     { return time.Second }
func (promptProvider) Interval(sessionLaunch) time.Duration { return time.Second }
func (promptProvider) Failures() discoveryFailures          { return testFailures }

func TestDiscoveryHandsTheLaunchPromptToTheProvider(t *testing.T) {
	recordSleeps(t)
	var seen sessionLaunch
	provider := promptProvider{seen: &seen}
	sessionProviders[provider.Mode()] = provider
	t.Cleanup(func() { delete(sessionProviders, provider.Mode()) })
	l := sessionLaunch{TaskID: "task-1", Prompt: "read the task file", CWD: "/work"}
	if id, err := discoverLaunchSession(provider, l, nil); err != nil || id != "seen" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if seen.Prompt != "read the task file" || seen.TaskID != "task-1" || seen.CWD != "/work" {
		t.Fatalf("provider saw %+v", seen)
	}
}

func TestRegisteringASessionProviderTwiceFails(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate session provider was accepted")
		}
	}()
	registerSessionProvider(generatedSessionProvider{mode: "generated", resumable: true})
}
