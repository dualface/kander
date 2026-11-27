package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/version"
)

// policyClient is one host: its own client home and scope configuration.
type policyClient struct {
	home, config string
}

func newPolicyClient(t *testing.T, serverURL string, adjust func(*config.Config)) policyClient {
	t.Helper()
	root := t.TempDir()
	c := policyClient{home: filepath.Join(root, "client"), config: filepath.Join(root, "config.json")}
	c.use(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.Server.URL = serverURL
	cfg.Project.Name = "golf"
	if adjust != nil {
		adjust(cfg)
	}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c policyClient) use(t *testing.T) {
	t.Setenv(EnvClientHome, c.home)
	t.Setenv(config.EnvConfig, c.config)
}

func isolatePolicyCommands(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	oldOut, oldErr := stdout, stderr
	oldIn, oldTerminal := policyPromptIn, policyTerminal
	t.Cleanup(func() { stdout, stderr, policyPromptIn, policyTerminal = oldOut, oldErr, oldIn, oldTerminal })
	policyTerminal = func() bool { return false }
}

// runCommand runs one command of this package and returns the exit code and both streams.
func runCommand(build func() *cobra.Command, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	stdout, stderr = &out, &errOut
	code, printed, reported := commandtest.Capture(func() int { return commandtest.Run(build(), args...) })
	return code, out.String() + printed, errOut.String() + reported
}

func TestRefreshPolicyWritesOnlyNewerVersions(t *testing.T) {
	home := t.TempDir()
	values := json.RawMessage(`{"git":{"branch_mode":"main-only"},"review_stages":{"PMQA":"auto","Security":"auto"},"rules":{},"agent_language":"en","issue":{"result_on_done":"auto"}}`)
	for _, step := range []struct {
		policy api.ProjectPolicy
		cached string
	}{
		{api.ProjectPolicy{Epoch: "e1", Revision: 2, Values: values}, "e1/2"},
		{api.ProjectPolicy{Epoch: "e1", Revision: 1, Values: values}, "e1/2"},
		{api.ProjectPolicy{Epoch: "e1", Revision: 3, Values: values}, "e1/3"},
		{api.ProjectPolicy{Epoch: "e2", Revision: 1, Values: values}, "e2/1"},
	} {
		cacheErr, err := RefreshPolicy(t.Context(), home, "http://127.0.0.1:1", "p-1", func() (*api.ProjectPolicy, error) { return &step.policy, nil })
		if err != nil || cacheErr != nil {
			t.Fatalf("%+v: %v %v", step.policy, cacheErr, err)
		}
		if got := cachedVersion(home, "http://127.0.0.1:1", "p-1"); got != step.cached {
			t.Fatalf("after %+v the cache holds %s, want %s", step.policy, got, step.cached)
		}
	}
}

func cachedVersion(home, url, id string) string {
	cache := config.ReadPolicyCache(home, url, id)
	switch cache.Status {
	case config.PolicyCacheSet, config.PolicyCacheUnset:
		return cache.Snapshot.Epoch + "/" + strconv.FormatUint(cache.Snapshot.Revision, 10)
	}
	return "none"
}

// fakePolicyServer answers the project and policy routes from memory, for the forms that need a
// server a real one cannot be: newer, older, or racing.
type fakePolicyServer struct {
	mu      sync.Mutex
	project bool // the project answer carries the policy field
	policy  api.ProjectPolicy
	puts    []api.PolicyWrite
	race    func()
}

func (f *fakePolicyServer) serve(t *testing.T) string {
	t.Helper()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: version.String()})
		case r.URL.Path == "/api/v1/projects":
			info := api.Project{ID: api.ProjectID("golf"), Name: "golf"}
			if f.project {
				policy := f.policy
				info.Policy = &policy
			}
			_ = json.NewEncoder(w).Encode(info)
		case strings.HasSuffix(r.URL.Path, "/policy") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.policy)
		case strings.HasSuffix(r.URL.Path, "/policy") && r.Method == http.MethodPut:
			var write api.PolicyWrite
			_ = json.NewDecoder(r.Body).Decode(&write)
			f.puts = append(f.puts, write)
			if f.race != nil {
				f.race()
				f.race = nil
			}
			if write.ExpectedRevision != f.policy.Revision {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodePolicyRevisionConflict, Message: "moved"})
				return
			}
			epoch := f.policy.Epoch
			if epoch == "" {
				epoch = "e-fake"
			}
			f.policy = api.ProjectPolicy{Epoch: epoch, Revision: f.policy.Revision + 1, Values: write.Values, UpdatedBy: write.Submitter}
			_ = json.NewEncoder(w).Encode(f.policy)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hs.Close)
	return hs.URL
}

func (f *fakePolicyServer) writes() []api.PolicyWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.PolicyWrite(nil), f.puts...)
}

const newerPolicyValues = `{"git":{"branch_mode":"main-only","future":"x"},"review_stages":{"large":{"PMQA":"auto","Security":"auto"},"small":{"PMQA":"auto","Security":"auto"}},` +
	`"rules":{"code":true,"collaboration":true,"git":true,"reporting":true,"review":true,"task_groups":true,"task_intake":true,"future_module":true},` +
	`"agent_language":"en","issue":{"result_on_done":"auto"},"future_section":{"a":1}}`

func TestPolicySetChangesOneValueAndKeepsUnknownKeys(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true, policy: api.ProjectPolicy{Epoch: "e1", Revision: 4, Values: json.RawMessage(newerPolicyValues)}}
	newPolicyClient(t, fake.serve(t), nil)
	code, out, errOut := runCommand(NewPolicyCommand, "set", "rules.review", "false")
	if code != 0 || !strings.Contains(out, "revision 5") {
		t.Fatalf("set: code=%d out=%q err=%q", code, out, errOut)
	}
	writes := fake.writes()
	if len(writes) != 1 || writes[0].ExpectedRevision != 4 {
		t.Fatalf("writes %+v", writes)
	}
	var sent, original map[string]any
	if err := json.Unmarshal(writes[0].Values, &sent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(newerPolicyValues), &original); err != nil {
		t.Fatal(err)
	}
	original["rules"].(map[string]any)["review"] = false
	if a, b := mustJSON(t, sent), mustJSON(t, original); a != b {
		t.Fatalf("written values\n%s\nwant\n%s", a, b)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPolicySetRefusesOtherKeysAndValues(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true, policy: api.ProjectPolicy{Epoch: "e1", Revision: 1, Values: json.RawMessage(newerPolicyValues)}}
	newPolicyClient(t, fake.serve(t), nil)
	for _, args := range [][]string{
		{"set", "launcher", "tmux"},
		{"set", "review_stages.large", "auto"},
		{"set", "review_stages.medium.PMQA", "auto"},
		{"set", "rules.future_module", "true"},
		{"set", "rules.review", "yes"},
		{"set", "git.branch_mode"},
	} {
		if code, _, errOut := runCommand(NewPolicyCommand, args...); code != 2 || errOut == "" {
			t.Errorf("%v: code=%d err=%q", args, code, errOut)
		}
	}
	if len(fake.writes()) != 0 {
		t.Fatal("a refused set wrote")
	}
}

func TestPolicySetReportsAConflictWithoutRetrying(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true, policy: api.ProjectPolicy{Epoch: "e1", Revision: 1, Values: json.RawMessage(newerPolicyValues)}}
	fake.race = func() { fake.policy.Revision = 2 }
	newPolicyClient(t, fake.serve(t), nil)
	code, _, errOut := runCommand(NewPolicyCommand, "set", "--json", "agent_language", "ja")
	if code == 0 || !strings.Contains(errOut, api.CodePolicyRevisionConflict) {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if len(fake.writes()) != 1 {
		t.Fatalf("the conflict was retried: %+v", fake.writes())
	}
}

func TestPolicyInitNeedsYesOrATerminal(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	newPolicyClient(t, fake.serve(t), nil)
	for _, args := range [][]string{{"init"}, {"init", "--json"}} {
		if code, _, errOut := runCommand(NewPolicyCommand, args...); code != 2 || !strings.Contains(errOut, "--yes") {
			t.Fatalf("%v: code=%d err=%q", args, code, errOut)
		}
	}
	policyTerminal = func() bool { return true }
	policyPromptIn = strings.NewReader("n\n")
	if code, _, errOut := runCommand(NewPolicyCommand, "init"); code != 1 || !strings.Contains(errOut, "git.branch_mode = main-develop") || !strings.Contains(errOut, "[y/N]") {
		t.Fatalf("declined: code=%d err=%q", code, errOut)
	}
	if len(fake.writes()) != 0 {
		t.Fatal("a declined or refused init wrote")
	}
	policyPromptIn = strings.NewReader("y\n")
	if code, out, errOut := runCommand(NewPolicyCommand, "init"); code != 0 || !strings.Contains(out, "revision 1") {
		t.Fatalf("confirmed: code=%d out=%q err=%q", code, out, errOut)
	}
	writes := fake.writes()
	if len(writes) != 1 || writes[0].ExpectedRevision != 0 {
		t.Fatalf("writes %+v", writes)
	}
	if _, err := config.ValidatePolicyValues(writes[0].Values); err != nil {
		t.Fatalf("init uploaded values the server refuses: %v", err)
	}
	if code, _, errOut := runCommand(NewPolicyCommand, "init", "--yes"); code != 1 || !strings.Contains(errOut, "kander policy set") {
		t.Fatalf("init of a set policy: code=%d err=%q", code, errOut)
	}
}

func TestPolicyCommandsReportAServerWithoutPolicies(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: false}
	newPolicyClient(t, fake.serve(t), nil)
	var remarks bytes.Buffer
	stderr = &remarks
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), cfg, t.TempDir()); err != nil || !strings.Contains(remarks.String(), "kander policy init") {
		t.Fatalf("open: err=%v remarks=%q", err, remarks.String())
	}
	for _, args := range [][]string{{"show", "--json"}, {"init", "--yes", "--json"}, {"set", "--json", "agent_language", "en"}} {
		code, _, errOut := runCommand(NewPolicyCommand, args...)
		if code != 1 || !strings.Contains(errOut, CodePolicyUnsupported) {
			t.Fatalf("%v: code=%d err=%q", args, code, errOut)
		}
	}
}

func TestInitOffersThePolicyInitializationOnlyInATerminal(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, func(cfg *config.Config) { cfg.Server.URL, cfg.Project.Name = "", "" })
	code, out, errOut := runCommand(NewInitCommand, "--server", url, "--project", "golf")
	if code != 0 || !strings.Contains(out, "golf") || !strings.Contains(errOut, "kander policy init") || len(fake.writes()) != 0 {
		t.Fatalf("non-interactive init: code=%d out=%q err=%q writes=%d", code, out, errOut, len(fake.writes()))
	}
	policyTerminal = func() bool { return true }
	policyPromptIn = strings.NewReader("y\n")
	code, out, errOut = runCommand(NewInitCommand)
	if code != 0 || !strings.Contains(errOut, "[y/N]") || !strings.Contains(out, "revision 1") || len(fake.writes()) != 1 {
		t.Fatalf("interactive init: code=%d out=%q err=%q writes=%d", code, out, errOut, len(fake.writes()))
	}
}

// A command that opens the project twice makes the unset remark once.
func TestPolicyRemarkIsMadeOncePerCommand(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	newPolicyClient(t, fake.serve(t), nil)
	twice := &cobra.Command{Use: "twice", RunE: func(*cobra.Command, []string) error {
		for range 2 {
			cfg, err := config.Load(false)
			if err != nil {
				return err
			}
			if _, err := Open(t.Context(), cfg, t.TempDir()); err != nil {
				return err
			}
		}
		return nil
	}}
	code, _, errOut := runCommand(func() *cobra.Command { return twice })
	if code != 0 || strings.Count(errOut, "kander policy init") != 1 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

// When the cache cannot be written, a later configuration read of the same process still uses
// the policy the server answered.
func TestConfigurationReadsAfterOpenUseTheServerAnswerWithoutACache(t *testing.T) {
	isolatePolicyCommands(t)
	values := strings.Replace(newerPolicyValues, `"agent_language":"en"`, `"agent_language":"ja"`, 1)
	fake := &fakePolicyServer{project: true, policy: api.ProjectPolicy{Epoch: "e1", Revision: 3, Values: json.RawMessage(values)}}
	client := newPolicyClient(t, fake.serve(t), nil)
	if err := os.MkdirAll(client.home, 0o700); err != nil {
		t.Fatal(err)
	}
	// A file where the cache directory belongs keeps every cache write from succeeding.
	if err := os.WriteFile(filepath.Join(client.home, "policies"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var remarks bytes.Buffer
	stderr = &remarks
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(remarks.String(), "could not be written") {
		t.Fatalf("no cache failure remark: %q", remarks.String())
	}
	again, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if again.AgentLanguage != "ja" || again.Policy.Source != config.PolicySourceServer || again.Policy.Revision != 3 {
		t.Fatalf("a later read used %q %+v", again.AgentLanguage, again.Policy)
	}
}

// A server that answers without a policy (an older one) is an unset policy for the whole command:
// a later configuration read does not take the values of a cache an earlier server wrote.
func TestServerWithoutPoliciesIsUnsetForLaterReadsOfTheCommand(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: false}
	url := fake.serve(t)
	client := newPolicyClient(t, url, func(cfg *config.Config) { cfg.AgentLanguage = "en" })
	values := strings.Replace(newerPolicyValues, `"agent_language":"en"`, `"agent_language":"ja"`, 1)
	id := api.ProjectID("golf")
	if _, err := config.StorePolicy(client.home, url, id, config.PolicySnapshot{Epoch: "e1", Revision: 2, Values: json.RawMessage(values)}); err != nil {
		t.Fatal(err)
	}
	if cfg, err := config.Load(false); err != nil || cfg.AgentLanguage != "ja" {
		t.Fatalf("the cached policy did not apply before the command: %v %+v", err, cfg)
	}
	var remarks bytes.Buffer
	stderr = &remarks
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	again, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentLanguage != "en" || again.AgentLanguage != "en" || again.Policy.Source != config.PolicySourceLocal {
		t.Fatalf("after Open: %q, later read %q %+v", cfg.AgentLanguage, again.AgentLanguage, again.Policy)
	}
	if code, _, errOut := runCommand(NewPolicyCommand, "show", "--json"); code != 1 || !strings.Contains(errOut, CodePolicyUnsupported) {
		t.Fatalf("policy show: code=%d err=%q", code, errOut)
	}
}

// policyStateServer stands in for the server: the policy it answers when a request reaches it.
type policyStateServer struct {
	mu     sync.Mutex
	policy api.ProjectPolicy
}

func (s *policyStateServer) set(p api.ProjectPolicy) { s.mu.Lock(); s.policy = p; s.mu.Unlock() }
func (s *policyStateServer) get() api.ProjectPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy
}

// slowRefresh starts a refresh whose request reads the server state at once and whose answer
// arrives only when release closes. started closes when the request reached the server.
func slowRefresh(t *testing.T, home string, server *policyStateServer, started, release chan struct{}) <-chan error {
	done := make(chan error, 1)
	go func() {
		cacheErr, err := RefreshPolicy(context.Background(), home, "http://127.0.0.1:1", "p-1", func() (*api.ProjectPolicy, error) {
			answer := server.get()
			close(started)
			<-release
			return &answer, nil
		})
		done <- errors.Join(cacheErr, err)
	}()
	return done
}

// Refreshes of one cache key run one at a time, so a request that the server answered before an
// initialization can never be stored after the answer of a later request: whichever refresh takes
// the lock first, the cache ends at the initialized policy.
func TestRefreshPolicyOrdersConcurrentRefreshesOfOneProject(t *testing.T) {
	values := json.RawMessage(`{"git":{"branch_mode":"main-only"},"review_stages":{"PMQA":"auto","Security":"auto"},"rules":{},"agent_language":"en","issue":{"result_on_done":"auto"}}`)
	initialized := api.ProjectPolicy{Epoch: "e1", Revision: 1, Values: values}
	for _, firstIsOlder := range []bool{true, false} {
		t.Run(map[bool]string{true: "the older answer is stored first", false: "the newer answer is stored first"}[firstIsOlder], func(t *testing.T) {
			home := t.TempDir()
			server := &policyStateServer{}
			if !firstIsOlder {
				server.set(initialized)
			}
			firstStarted, releaseFirst := make(chan struct{}), make(chan struct{})
			first := slowRefresh(t, home, server, firstStarted, releaseFirst)
			<-firstStarted
			if firstIsOlder {
				// The project is initialized after the first request was answered.
				server.set(initialized)
			}
			secondStarted, releaseSecond := make(chan struct{}), make(chan struct{})
			close(releaseSecond)
			second := slowRefresh(t, home, server, secondStarted, releaseSecond)
			select {
			case <-secondStarted:
				t.Fatal("a second request of the same project ran while the first refresh held the cache")
			case <-time.After(200 * time.Millisecond):
			}
			close(releaseFirst)
			for _, done := range []<-chan error{first, second} {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			if got := cachedVersion(home, "http://127.0.0.1:1", "p-1"); got != "e1/1" {
				t.Fatalf("the cache holds %s, want the initialized e1/1", got)
			}
		})
	}
}

// A project removed and imported again starts unset and then takes a new epoch: both answers
// replace the cache.
func TestRefreshPolicyFollowsAReimport(t *testing.T) {
	home := t.TempDir()
	values := json.RawMessage(`{"git":{"branch_mode":"main-only"},"review_stages":{"PMQA":"auto","Security":"auto"},"rules":{},"agent_language":"en","issue":{"result_on_done":"auto"}}`)
	for _, step := range []struct {
		policy api.ProjectPolicy
		cached string
	}{
		{api.ProjectPolicy{Epoch: "e1", Revision: 3, Values: values}, "e1/3"},
		{api.ProjectPolicy{}, "/0"},
		{api.ProjectPolicy{Epoch: "e2", Revision: 1, Values: values}, "e2/1"},
	} {
		if cacheErr, err := RefreshPolicy(t.Context(), home, "http://127.0.0.1:1", "p-1", func() (*api.ProjectPolicy, error) { return &step.policy, nil }); err != nil || cacheErr != nil {
			t.Fatal(cacheErr, err)
		}
		if got := cachedVersion(home, "http://127.0.0.1:1", "p-1"); got != step.cached {
			t.Fatalf("after %+v the cache holds %s, want %s", step.policy, got, step.cached)
		}
	}
}

// When another process keeps the cache lock past the wait, the request still runs and its answer
// applies to the command, but nothing is stored.
func TestRefreshPolicyWithoutTheLockStoresNothing(t *testing.T) {
	home := t.TempDir()
	previous := policyLockWait
	policyLockWait = 100 * time.Millisecond
	t.Cleanup(func() { policyLockWait = previous })
	held, err := config.BeginPolicyRefresh(t.Context(), home, "http://127.0.0.1:1", "p-1")
	if err != nil {
		t.Fatal(err)
	}
	defer held.End()
	fetched := false
	cacheErr, err := RefreshPolicy(t.Context(), home, "http://127.0.0.1:1", "p-1", func() (*api.ProjectPolicy, error) {
		fetched = true
		return &api.ProjectPolicy{}, nil
	})
	if err != nil || cacheErr == nil || !fetched {
		t.Fatalf("fetched=%v cacheErr=%v err=%v", fetched, cacheErr, err)
	}
	if got := cachedVersion(home, "http://127.0.0.1:1", "p-1"); got != "none" {
		t.Fatalf("the cache holds %s without the lock", got)
	}
}

// A command with a short deadline whose project cache lock another process holds, waiting for a
// slow answer, still opens the project: the lock wait takes only a share of the deadline, the
// registration runs without the lock against a server that answers at once, the answer applies,
// and the cache failure is only a remark.
func TestOpenWithAShortDeadlineDoesNotWaitOutAHeldPolicyLock(t *testing.T) {
	isolatePolicyCommands(t)
	values := strings.Replace(newerPolicyValues, `"agent_language":"en"`, `"agent_language":"ja"`, 1)
	fake := &fakePolicyServer{project: true, policy: api.ProjectPolicy{Epoch: "e1", Revision: 1, Values: json.RawMessage(values)}}
	url := fake.serve(t)
	client := newPolicyClient(t, url, nil)
	held, err := config.BeginPolicyRefresh(t.Context(), client.home, url, api.ProjectID("golf"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End()
	var remarks bytes.Buffer
	stderr = &remarks
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	if _, err := Open(ctx, cfg, t.TempDir()); err != nil {
		t.Fatalf("Open failed after %s: %v", time.Since(started), err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Open took %s; the lock wait used the deadline", elapsed)
	}
	if cfg.AgentLanguage != "ja" || cfg.Policy.Source != config.PolicySourceServer {
		t.Fatalf("the server policy did not apply: %q %+v", cfg.AgentLanguage, cfg.Policy)
	}
	if !strings.Contains(remarks.String(), "could not be written") {
		t.Fatalf("no cache remark: %q", remarks.String())
	}
	if got := cachedVersion(client.home, url, api.ProjectID("golf")); got != "none" {
		t.Fatalf("the cache was written without the lock: %s", got)
	}
}

// An explicit cancellation before the lock ends the refresh without its request.
func TestRefreshPolicyStopsOnCancellationBeforeTheLock(t *testing.T) {
	home := t.TempDir()
	held, err := config.BeginPolicyRefresh(t.Context(), home, "http://127.0.0.1:1", "p-1")
	if err != nil {
		t.Fatal(err)
	}
	defer held.End()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fetched := false
	_, err = RefreshPolicy(ctx, home, "http://127.0.0.1:1", "p-1", func() (*api.ProjectPolicy, error) {
		fetched = true
		return &api.ProjectPolicy{}, nil
	})
	if !errors.Is(err, context.Canceled) || fetched {
		t.Fatalf("err=%v fetched=%v", err, fetched)
	}
}
