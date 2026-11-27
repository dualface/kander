package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// EnvClientHome relocates the private client directory (tests, several
// identities on one machine). internal/remote names the same variable.
const EnvClientHome = "KANDER_CLIENT_HOME"

// ClientHome returns the private client directory of this user: the
// KANDER_CLIENT_HOME directory, or client/ below the global share directory.
func ClientHome() (string, error) {
	if dir := os.Getenv(EnvClientHome); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", errors.New(EnvClientHome + " must be an absolute path")
		}
		return dir, nil
	}
	paths, err := GlobalInstallPaths()
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.ShareDir, "client"), nil
}

// ProjectIDForName derives the service project ID of a project name. It copies
// api.ProjectID, which this package cannot import; an api test pins the two.
func ProjectIDForName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "p-" + hex.EncodeToString(sum[:8])
}

// PolicyOrigin is the canonical server origin a policy cache is keyed by: the
// scheme and host in lower case, with the path of the URL and no trailing
// slash, so two spellings of one server URL share one cache.
func PolicyOrigin(serverURL string) string {
	u, err := url.Parse(strings.TrimRight(serverURL, "/"))
	if err != nil || u.Host == "" {
		return strings.TrimRight(serverURL, "/")
	}
	origin := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	return origin + strings.TrimRight(u.EscapedPath(), "/")
}

const (
	policyCacheDir    = "policies"
	policyCacheSchema = 1
)

// PolicyCachePath is the cache file of one project of one server below the
// client directory home.
func PolicyCachePath(home, serverURL, projectID string) string {
	sum := sha256.Sum256([]byte(PolicyOrigin(serverURL) + "\x00" + projectID))
	return filepath.Join(home, policyCacheDir, hex.EncodeToString(sum[:])+".json")
}

// PolicySnapshot is one project policy as a server answered it. Revision 0
// means the project has no policy; Values is then empty.
type PolicySnapshot struct {
	Epoch     string          `json:"epoch,omitempty"`
	Revision  uint64          `json:"revision"`
	Values    json.RawMessage `json:"values,omitempty"`
	UpdatedAt time.Time       `json:"updated_at,omitzero"`
	UpdatedBy string          `json:"updated_by,omitempty"`
}

// policyCacheRecord is the cache file: the snapshot and the server and project
// it belongs to, so a file found under another name is never applied.
type policyCacheRecord struct {
	Schema    int    `json:"schema"`
	Origin    string `json:"origin"`
	ProjectID string `json:"project_id"`
	PolicySnapshot
	StoredAt time.Time `json:"stored_at"`
}

// PolicyCacheStatus says what a read of the policy cache found.
type PolicyCacheStatus int

const (
	// PolicyCacheMissing: there is no cache file.
	PolicyCacheMissing PolicyCacheStatus = iota
	// PolicyCacheInvalid: the file cannot be read, decoded, or validated.
	PolicyCacheInvalid
	// PolicyCacheUnset: the server answered that the project has no policy.
	PolicyCacheUnset
	// PolicyCacheSet: the cache holds a policy.
	PolicyCacheSet
)

// PolicyCache is the result of a cache read.
type PolicyCache struct {
	Status   PolicyCacheStatus
	Snapshot PolicySnapshot
	// Values are the decoded values of a set policy, and Ignored the keys
	// of the values this version does not know.
	Values  PolicyValues
	Ignored []string
	// Err is why an invalid cache was refused.
	Err error
}

// ReadPolicyCache reads the cache of one project of one server. It never
// fails: a file that cannot be read or does not validate is PolicyCacheInvalid
// with the reason, and the next refresh replaces it.
func ReadPolicyCache(home, serverURL, projectID string) PolicyCache {
	path := PolicyCachePath(home, serverURL, projectID)
	data, found, err := readPolicyCacheFile(home, path)
	if err != nil {
		return PolicyCache{Status: PolicyCacheInvalid, Err: err}
	}
	if !found {
		return PolicyCache{Status: PolicyCacheMissing}
	}
	record, err := decodePolicyCache(data, serverURL, projectID)
	if err != nil {
		return PolicyCache{Status: PolicyCacheInvalid, Err: err}
	}
	if record.Revision == 0 {
		return PolicyCache{Status: PolicyCacheUnset, Snapshot: record.PolicySnapshot}
	}
	values, ignored, err := ReadPolicyValues(record.Values)
	if err != nil {
		return PolicyCache{Status: PolicyCacheInvalid, Snapshot: record.PolicySnapshot, Err: err}
	}
	return PolicyCache{Status: PolicyCacheSet, Snapshot: record.PolicySnapshot, Values: values, Ignored: ignored}
}

func readPolicyCacheFile(home, path string) ([]byte, bool, error) {
	exists, err := fs.DirectoryExists(home, filepath.Dir(path))
	if err != nil || !exists {
		return nil, false, err
	}
	return fs.ReadRegularFileIfExists(home, path)
}

func decodePolicyCache(data []byte, serverURL, projectID string) (policyCacheRecord, error) {
	var record policyCacheRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	switch {
	case record.Schema != policyCacheSchema:
		return record, errors.New(Text("policy.cache_schema", record.Schema))
	case record.Origin != PolicyOrigin(serverURL) || record.ProjectID != projectID:
		return record, errors.New(Text("policy.cache_foreign"))
	case record.Revision > 0 && record.Epoch == "":
		return record, errors.New(Text("policy.cache_no_epoch"))
	}
	return record, nil
}

// PolicyRefresh holds the lock of the policy cache of one project from before a request to the
// server until its answer is stored, so the refreshes of one cache key run one at a time: a
// request starts only after every earlier refresh stored its answer, and its answer, read from a
// later state of the server, is the newest this key has seen. That orders answers of different
// epochs and unset answers, which the policy version alone cannot.
type PolicyRefresh struct {
	home, serverURL, projectID string
	file                       *fs.AppendFile
	lock                       *fs.ExclusiveLock
}

// BeginPolicyRefresh takes the lock of the policy cache of one project, waiting until ctx ends.
// The caller sends its request, stores the answer with Store, and calls End.
func BeginPolicyRefresh(ctx context.Context, home, serverURL, projectID string) (*PolicyRefresh, error) {
	path := PolicyCachePath(home, serverURL, projectID)
	if err := ensurePrivateDir(home, filepath.Dir(path)); err != nil {
		return nil, err
	}
	file, err := fs.OpenAppendFile(home, path+".lock")
	if err != nil {
		return nil, err
	}
	lock, err := fs.LockExclusiveContext(ctx, file.File)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &PolicyRefresh{home: home, serverURL: serverURL, projectID: projectID, file: file, lock: lock}, nil
}

// Store writes the answer of the request sent under this refresh. A cache that cannot be read
// counts as no cache; an answer with the cached policy version changes nothing. A snapshot of
// revision 0 replaces a cached policy with the unset record, so the values of a project that was
// removed and imported again are not kept. It reports whether it wrote.
func (r *PolicyRefresh) Store(snapshot PolicySnapshot) (bool, error) {
	current := ReadPolicyCache(r.home, r.serverURL, r.projectID)
	if current.Status == PolicyCacheSet || current.Status == PolicyCacheUnset {
		if !policyVersionNewer(snapshot, current.Snapshot) {
			return false, nil
		}
	}
	record := policyCacheRecord{Schema: policyCacheSchema, Origin: PolicyOrigin(r.serverURL), ProjectID: r.projectID, PolicySnapshot: snapshot, StoredAt: time.Now().UTC()}
	if record.Revision == 0 {
		record.PolicySnapshot = PolicySnapshot{}
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return false, err
	}
	if err := fs.WriteTextAtomic(r.home, PolicyCachePath(r.home, r.serverURL, r.projectID), string(data)+"\n", true); err != nil {
		return false, err
	}
	return true, nil
}

// End releases the lock of the refresh.
func (r *PolicyRefresh) End() {
	_ = r.lock.Unlock()
	_ = r.file.Close()
}

// StorePolicy stores a snapshot that no request of this process produced under the lock, such as
// a test fixture: it takes the lock, writes the snapshot when its policy version is newer than
// the cached one (another epoch, or the same epoch with a larger revision), and releases it.
// Answers of the server go through BeginPolicyRefresh instead.
func StorePolicy(home, serverURL, projectID string, snapshot PolicySnapshot) (bool, error) {
	refresh, err := BeginPolicyRefresh(context.Background(), home, serverURL, projectID)
	if err != nil {
		return false, err
	}
	defer refresh.End()
	return refresh.Store(snapshot)
}

func policyVersionNewer(next, base PolicySnapshot) bool {
	return next.Epoch != base.Epoch || next.Revision > base.Revision
}

// ensurePrivateDir creates dir below home, both private to the current user.
func ensurePrivateDir(home, dir string) error {
	if err := os.MkdirAll(filepath.Dir(home), 0o700); err != nil {
		return err
	}
	if err := fs.EnsurePrivateDirectory(filepath.Dir(home), home, true); err != nil {
		return err
	}
	return fs.EnsurePrivateDirectory(home, dir, true)
}

// Sources of the project policy values of a loaded configuration.
const (
	// PolicySourceLocal: the values come from the configuration files.
	PolicySourceLocal = "local"
	// PolicySourceServerCache: the values come from the policy cache.
	PolicySourceServerCache = "server_cache"
	// PolicySourceServer: the values come from the server answer of this run.
	PolicySourceServer = "server"
)

// Notice codes of the project policy.
const (
	NoticePolicyUnset        = "project_policy_unset"
	NoticePolicyCacheMissing = "project_policy_cache_missing"
	NoticePolicyCacheInvalid = "project_policy_cache_invalid"
	NoticePolicyIgnored      = "project_policy_ignored"
	NoticePolicyOverride     = "project_policy_override"
)

// PolicyState says where the project policy values of a loaded configuration
// come from. It is not part of the configuration file.
type PolicyState struct {
	Source    string
	Epoch     string
	Revision  uint64
	Notices   []PolicyNotice
	Overrides []PolicyOverride
}

// PolicyNotice is one remark about the project policy of a configuration.
type PolicyNotice struct {
	Code    string
	Message string
}

// PolicyStatus is the project_policy object of kander config --json.
type PolicyStatus struct {
	Epoch    string `json:"epoch,omitempty"`
	Revision uint64 `json:"revision"`
	Source   string `json:"source"`
}

// Status returns the project_policy object of the state.
func (s PolicyState) Status() PolicyStatus {
	source := s.Source
	if source == "" {
		source = PolicySourceLocal
	}
	return PolicyStatus{Epoch: s.Epoch, Revision: s.Revision, Source: source}
}

// Applied reports whether the policy values of the configuration come from a
// server policy.
func (s PolicyState) Applied() bool {
	return s.Source == PolicySourceServerCache || s.Source == PolicySourceServer
}

// applyPolicyCache replaces the policy values of cfg with the cached policy of
// its project, when it has one, and records the state in cfg.Policy with the
// values of the layers (the configuration files) that the policy replaces. A
// configuration without a project binding has no policy and no remark.
func applyPolicyCache(cfg *Config, layers []policyLayer) {
	if cfg.Project.Name == "" {
		return
	}
	home, err := ClientHome()
	if err != nil {
		cfg.Policy.Notices = []PolicyNotice{{NoticePolicyCacheInvalid, Text("policy.cache_invalid", err.Error())}}
		return
	}
	serverURL, projectID := ServerURL(cfg), ProjectIDForName(cfg.Project.Name)
	if snapshot, ok := rememberedPolicy(home, serverURL, projectID); ok {
		// This command already read the policy from the server: use that answer, whatever
		// the cache holds, so every consumer of one command sees the same values even when
		// the cache could not be written.
		ApplyServerPolicy(cfg, snapshot)
		if cfg.Policy.Source == PolicySourceServer {
			values, _, _ := ReadPolicyValues(snapshot.Values)
			addPolicyOverrides(cfg, values, layers)
		}
		return
	}
	cache := ReadPolicyCache(home, serverURL, projectID)
	ApplyPolicyCache(cfg, cache)
	if cache.Status == PolicyCacheSet {
		addPolicyOverrides(cfg, cache.Values, layers)
	}
}

// addPolicyOverrides lists the file values the applied policy values replace.
func addPolicyOverrides(cfg *Config, values PolicyValues, layers []policyLayer) {
	cfg.Policy.Overrides = policyOverrides(values, layers)
	for _, override := range cfg.Policy.Overrides {
		cfg.Policy.Notices = append(cfg.Policy.Notices, PolicyNotice{NoticePolicyOverride, PolicyOverrideText(override)})
	}
}

// rememberedPolicies holds, per cache file, the last policy a server answered in the running
// command. command.Execute forgets them when a command starts.
var rememberedPolicies sync.Map

// ForgetServerPolicies drops the policies RememberServerPolicy recorded.
func ForgetServerPolicies() { rememberedPolicies.Clear() }

// RememberServerPolicy records the policy a server answered for one project in the running
// command. Load prefers it over the cache file until the next command starts.
func RememberServerPolicy(home, serverURL, projectID string, snapshot PolicySnapshot) {
	rememberedPolicies.Store(PolicyCachePath(home, serverURL, projectID), snapshot)
}

func rememberedPolicy(home, serverURL, projectID string) (PolicySnapshot, bool) {
	value, ok := rememberedPolicies.Load(PolicyCachePath(home, serverURL, projectID))
	if !ok {
		return PolicySnapshot{}, false
	}
	return value.(PolicySnapshot), true
}

// ApplyPolicyCache applies the result of a cache read to cfg the way Load
// does. A caller that holds the project ID instead of the project name, such
// as the background worker with an execution credential, reads the cache with
// ReadPolicyCache and applies it here.
func ApplyPolicyCache(cfg *Config, cache PolicyCache) {
	switch cache.Status {
	case PolicyCacheMissing:
		cfg.Policy = localPolicy(NoticePolicyCacheMissing, Text("policy.cache_missing"))
	case PolicyCacheInvalid:
		cfg.Policy = localPolicy(NoticePolicyCacheInvalid, Text("policy.cache_invalid", cache.Err.Error()))
	case PolicyCacheUnset:
		cfg.Policy = localPolicy(NoticePolicyUnset, Text("policy.unset"))
	case PolicyCacheSet:
		applyPolicyValues(cfg, PolicySourceServerCache, cache.Snapshot, cache.Values, cache.Ignored)
	}
}

// ApplyServerPolicy applies a policy a server just answered to cfg and
// records the server as the source; it replaces the values of an earlier cache
// read. An unset policy, or values this version cannot read, put the file
// values back with a remark: the cache read may have replaced them with the
// values of a policy the server no longer has.
func ApplyServerPolicy(cfg *Config, snapshot PolicySnapshot) {
	if snapshot.Revision == 0 {
		LocalPolicyValues(cfg).ApplyTo(cfg)
		cfg.Policy = localPolicy(NoticePolicyUnset, Text("policy.unset"))
		return
	}
	values, ignored, err := ReadPolicyValues(snapshot.Values)
	if err != nil {
		LocalPolicyValues(cfg).ApplyTo(cfg)
		cfg.Policy = localPolicy(NoticePolicyCacheInvalid, Text("policy.server_invalid", err.Error()))
		return
	}
	applyPolicyValues(cfg, PolicySourceServer, snapshot, values, ignored)
}

// LocalPolicyValues are the policy values of the configuration files and the
// defaults, before any policy replaced them: what kander policy init uploads.
func LocalPolicyValues(cfg *Config) PolicyValues {
	if cfg.policyLocal != nil {
		return *cfg.policyLocal
	}
	return PolicyValuesOf(cfg)
}

func localPolicy(code, message string) PolicyState {
	return PolicyState{Source: PolicySourceLocal, Notices: []PolicyNotice{{Code: code, Message: message}}}
}

func applyPolicyValues(cfg *Config, source string, snapshot PolicySnapshot, values PolicyValues, ignored []string) {
	if cfg.policyLocal == nil {
		local := PolicyValuesOf(cfg)
		cfg.policyLocal = &local
	}
	values.ApplyTo(cfg)
	cfg.layered = true
	state := PolicyState{Source: source, Epoch: snapshot.Epoch, Revision: snapshot.Revision}
	for _, path := range ignored {
		state.Notices = append(state.Notices, PolicyNotice{NoticePolicyIgnored, Text("policy.ignored_key", path)})
	}
	cfg.Policy = state
}

// PolicyOverrideText is the one-line remark about a replaced file value.
func PolicyOverrideText(o PolicyOverride) string {
	return Text("policy.override", o.Path, o.Source, o.Local, o.Policy)
}
