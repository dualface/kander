package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
)

// CodePolicyUnsupported reports a server that keeps no project policies.
const CodePolicyUnsupported = "policy_unsupported"

// policyLockWait is the longest wait for the policy cache lock of a refresh; tests change it.
var policyLockWait = 2 * time.Second

// policyLockBudget is the wait for the policy cache lock under ctx: policyLockWait, and at most a
// quarter of the time ctx has left, so a refresh that cannot get the lock still leaves its
// request and the rest of the command most of their time.
func policyLockBudget(ctx context.Context) time.Duration {
	wait := policyLockWait
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(deadline)/4)
	}
	return max(wait, 0)
}

// PolicySnapshot converts a policy answer to the form the policy cache keeps.
func PolicySnapshot(p api.ProjectPolicy) config.PolicySnapshot {
	return config.PolicySnapshot{Epoch: p.Epoch, Revision: p.Revision, Values: p.Values, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy}
}

// RefreshPolicy is the policy refresh function. It runs fetch, the request that answers the policy
// of one project, while it holds the lock of that project's policy cache in the client directory
// home, then stores the answer when its policy version is newer than the cached one. The
// refreshes of one cache key therefore run one at a time, and an older answer of a concurrent
// process never replaces a newer cache. serverURL and projectID key the cache: a command passes
// its configured server URL and the project ID of its session, the worker the ServerURL and
// ProjectID of an execution credential.
//
// fetch returns nil for a server without project policies: nothing is stored, and the rest of
// the command reads the policy as unset. The answer also stays in this process for the running
// command (config.RememberServerPolicy), so a later configuration read uses it even when the cache
// cannot be written. err is the error of fetch, or of ctx when it ended before the lock; cacheErr
// is a lock or write failure of the cache, which never fails the caller. When the lock cannot be
// taken within its short wait, fetch runs without it and nothing is stored.
func RefreshPolicy(ctx context.Context, home, serverURL, projectID string, fetch func() (*api.ProjectPolicy, error)) (cacheErr, err error) {
	// The wait for the lock has a short bound of its own (policyLockBudget): a refresh another
	// process holds up, waiting for a slow answer, runs its request without the lock and stores
	// nothing, instead of spending the command's time. An explicit cancellation still ends it.
	lockCtx, cancel := context.WithTimeout(ctx, policyLockBudget(ctx))
	refresh, cacheErr := config.BeginPolicyRefresh(lockCtx, home, serverURL, projectID)
	cancel()
	if refresh != nil {
		defer refresh.End()
	} else if err := ctx.Err(); err != nil {
		return cacheErr, err
	}
	policy, err := fetch()
	if err != nil {
		return cacheErr, err
	}
	var snapshot config.PolicySnapshot
	if policy != nil {
		snapshot = PolicySnapshot(*policy)
	}
	config.RememberServerPolicy(home, serverURL, projectID, snapshot)
	if policy == nil || refresh == nil {
		return cacheErr, nil
	}
	_, cacheErr = refresh.Store(snapshot)
	return cacheErr, nil
}

// FetchPolicy reads the policy of the session's project from the server,
// refreshes the policy cache with it, and returns it. A failed cache write is
// returned as cacheErr beside the policy.
func (s *Session) FetchPolicy(ctx context.Context) (policy api.ProjectPolicy, cacheErr error, err error) {
	cacheErr, err = RefreshPolicy(ctx, s.Home, s.ServerURL, s.Info.ID, func() (*api.ProjectPolicy, error) {
		answer, err := s.Project.Policy(ctx)
		policy = answer
		return &answer, err
	})
	if err != nil {
		return api.ProjectPolicy{}, nil, err
	}
	return policy, cacheErr, nil
}

// CredentialPolicy reads the cached policy of the project of an execution
// credential; the worker holds the project ID, not the project name.
func CredentialPolicy(home string, c Credential) config.PolicyCache {
	return config.ReadPolicyCache(home, c.ServerURL, c.ProjectID)
}

// applySessionPolicy applies the policy of the registration answer to cfg, so the rest of the
// command uses the server's values; Open refreshed the cache with the same answer. A server
// without project policies counts as an unset policy. A failed cache write is only a remark. The
// remarks about the policy go out unless quiet.
func applySessionPolicy(cfg *config.Config, info api.Project, cacheErr error, quiet bool) {
	if cacheErr != nil {
		remark("project_policy_cache_failed", config.Text("policy.cache_write_failed", cacheErr.Error()))
	}
	var snapshot config.PolicySnapshot
	if info.Policy != nil {
		snapshot = PolicySnapshot(*info.Policy)
	}
	config.ApplyServerPolicy(cfg, snapshot)
	if quiet {
		return
	}
	for _, notice := range cfg.Policy.Notices {
		// A command that opens the project more than once (start, a takeover resume) makes
		// each policy remark once.
		if notice.Code != config.NoticePolicyOverride && command.FirstRemark("policy\x00"+notice.Code+"\x00"+notice.Message) {
			remark(notice.Code, notice.Message)
		}
	}
}

// NewPolicyCommand builds kander policy.
func NewPolicyCommand() *cobra.Command {
	show := &cobra.Command{
		Use:  "show",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return runPolicyShow() },
	}
	var yes bool
	initialize := &cobra.Command{
		Use:  "init",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error {
			// Checked before anything is read or written; after the project gate, so an
			// unbound checkout is told about its binding first.
			if !yes && !policyInteractive() {
				return command.Usage("policy.init_needs_yes")
			}
			return runPolicyInit(yes)
		},
	}
	initialize.Flags().BoolVar(&yes, "yes", false, "")
	set := &cobra.Command{
		Use: "set <key> <value>",
		Args: func(c *cobra.Command, args []string) error {
			if err := command.Args(2, 2)(c, args); err != nil {
				return err
			}
			if !slices.Contains(config.PolicyLeafPaths(), args[0]) {
				return command.Usage("policy.set_unknown_key", command.Escape(args[0]), strings.Join(config.PolicyLeafPaths(), ", "))
			}
			if _, err := policyLeafValue(args[0], args[1]); err != nil {
				return err
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error { return runPolicySet(args[0], args[1]) },
	}
	return command.Group("policy", command.RequireProject(show), command.RequireProject(initialize), command.RequireProject(set))
}

// The prompt streams and the terminal test of kander policy init and of the
// policy step of kander init; tests replace them.
var (
	policyPromptIn io.Reader = os.Stdin
	policyTerminal           = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
	}
)

// policyInteractive reports whether a person can answer the initialization
// question: a terminal on stdin and stderr, no --json, and no Agent execution.
func policyInteractive() bool {
	_, execution := os.LookupEnv(EnvExecutionID)
	_, coordinator := os.LookupEnv(EnvCoordinatorExecutionID)
	return !command.JSON() && !execution && !coordinator && policyTerminal()
}

// openPolicySession opens the project quietly: the policy commands report the
// policy themselves. A server without project policies is refused.
func openPolicySession() (context.Context, *Session, *config.Config, func(), error) {
	base, cancel := commandContext()
	cfg, err := config.Load(false)
	if err != nil {
		cancel()
		return nil, nil, nil, nil, err
	}
	dir, _ := os.Getwd()
	s, err := OpenWith(base, cfg, dir, OpenOptions{QuietPolicy: true})
	if err != nil {
		cancel()
		return nil, nil, nil, nil, err
	}
	if s.Info.Policy == nil {
		cancel()
		return nil, nil, nil, nil, command.Coded(CodePolicyUnsupported, errors.New(config.Text("policy.server_unsupported", s.ServerURL)))
	}
	ctx, finish := s.OperationContext(base)
	return ctx, s, cfg, func() { finish(); cancel() }, nil
}

func runPolicyShow() error {
	ctx, s, _, release, err := openPolicySession()
	if err != nil {
		return fail(err)
	}
	defer release()
	policy, cacheErr, err := s.FetchPolicy(ctx)
	if err != nil {
		return fail(err)
	}
	reportCacheFailure(cacheErr)
	if command.JSON() {
		return command.Exit(command.Result(policy))
	}
	if policy.Revision == 0 {
		fmt.Fprintln(stdout, config.Text("policy.show_unset"))
		return nil
	}
	fmt.Fprintln(stdout, config.Text("policy.show_version", policy.Revision, policy.Epoch))
	fmt.Fprintln(stdout, config.Text("policy.show_updated", policy.UpdatedBy, policy.UpdatedAt.Format("2006-01-02 15:04:05Z07:00")))
	values, ignored, err := config.ReadPolicyValues(policy.Values)
	if err != nil {
		return fail(err)
	}
	printPolicyValues(stdout, values)
	for _, path := range ignored {
		remark(config.NoticePolicyIgnored, config.Text("policy.ignored_key", path))
	}
	return nil
}

func runPolicyInit(yes bool) error {
	ctx, s, cfg, release, err := openPolicySession()
	if err != nil {
		return fail(err)
	}
	defer release()
	policy, cacheErr, err := s.FetchPolicy(ctx)
	if err != nil {
		return fail(err)
	}
	reportCacheFailure(cacheErr)
	if policy.Revision > 0 {
		return fail(command.Coded("policy_already_set", errors.New(config.Text("policy.init_already_set", policy.Revision))))
	}
	written, err := initializePolicy(ctx, s, config.LocalPolicyValues(cfg), !yes)
	if err != nil {
		return fail(err)
	}
	if written == nil {
		return command.Exit(1)
	}
	return reportPolicyWrite(*written)
}

// initializePolicy uploads values as the first policy of the project. With
// ask, it shows the values and waits for a yes first; a no writes nothing and
// returns no policy.
func initializePolicy(ctx context.Context, s *Session, values config.PolicyValues, ask bool) (*api.ProjectPolicy, error) {
	if ask {
		fmt.Fprintln(stderr, config.Text("policy.init_preview", s.Info.Name))
		printPolicyValues(stderr, values)
		fmt.Fprint(stderr, config.Text("policy.init_confirm"))
		line, err := bufio.NewReader(policyPromptIn).ReadString('\n')
		if answer := strings.TrimSpace(line); err != nil || (answer != "y" && answer != "Y") {
			fmt.Fprintln(stderr, config.Text("policy.init_cancelled"))
			return nil, nil
		}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return putPolicy(ctx, s, api.PolicyWrite{ExpectedRevision: 0, Values: data})
}

func runPolicySet(key, raw string) error {
	value, err := policyLeafValue(key, raw)
	if err != nil {
		return err
	}
	ctx, s, _, release, err := openPolicySession()
	if err != nil {
		return fail(err)
	}
	defer release()
	policy, cacheErr, err := s.FetchPolicy(ctx)
	if err != nil {
		return fail(err)
	}
	reportCacheFailure(cacheErr)
	if policy.Revision == 0 {
		return fail(command.Coded(config.NoticePolicyUnset, errors.New(config.Text("policy.set_unset"))))
	}
	values, err := setPolicyLeaf(policy.Values, key, value)
	if err != nil {
		return fail(err)
	}
	written, err := putPolicy(ctx, s, api.PolicyWrite{ExpectedRevision: policy.Revision, Values: values})
	if err != nil {
		return fail(err)
	}
	return reportPolicyWrite(*written)
}

// putPolicy writes the policy with one idempotency key for every transport
// retry and refreshes the cache with the answer. A revision conflict is
// returned as it is: the command never retries it.
func putPolicy(ctx context.Context, s *Session, write api.PolicyWrite) (*api.ProjectPolicy, error) {
	write.Submitter = GitSubmitter(ctx)
	var written api.ProjectPolicy
	cacheErr, err := RefreshPolicy(ctx, s.Home, s.ServerURL, s.Info.ID, func() (*api.ProjectPolicy, error) {
		answer, err := s.Project.PutPolicy(ctx, client.NewKey(), write)
		written = answer
		return &answer, err
	})
	if err != nil {
		return nil, err
	}
	reportCacheFailure(cacheErr)
	return &written, nil
}

func reportPolicyWrite(policy api.ProjectPolicy) error {
	if command.JSON() {
		return command.Exit(command.Result(policy))
	}
	fmt.Fprintln(stdout, config.Text("policy.written", policy.Revision, policy.Epoch))
	return nil
}

func reportCacheFailure(err error) {
	if err != nil {
		remark("project_policy_cache_failed", config.Text("policy.cache_write_failed", err.Error()))
	}
}

func printPolicyValues(w io.Writer, values config.PolicyValues) {
	for _, leaf := range values.Leaves() {
		fmt.Fprintf(w, "  %s = %s\n", leaf.Path, leaf.Value)
	}
}

// policyLeafValue is the JSON value of one kander policy set argument: a
// boolean for a rule module, a string otherwise.
func policyLeafValue(key, raw string) (any, error) {
	if !strings.HasPrefix(key, config.PolicyKeyRules+".") {
		return raw, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil || (raw != "true" && raw != "false") {
		return nil, command.Usage("policy.set_boolean", command.Escape(key), command.Escape(raw))
	}
	return enabled, nil
}

// setPolicyLeaf changes one value of the policy document and keeps every other
// key, those this version does not know included.
func setPolicyLeaf(values json.RawMessage, key string, value any) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(values))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil || doc == nil {
		return nil, errors.New(config.Text("policy.values_not_object"))
	}
	parts := strings.Split(key, ".")
	current := doc
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
	return json.Marshal(doc)
}
