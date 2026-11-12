// Package remote binds a kander client process to its server project: it
// reads server.url and project.name, keeps the persistent client identity and
// the execution credentials of this host in a private directory, and opens
// the REST client. It never touches a board directory.
package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// Environment variables. EnvExecutionID binds a process to one execution, so
// an agent started for an earlier generation can never pick up the
// credential of a later one. EnvClientHome relocates the private client
// directory (tests, several identities on one machine).
const (
	EnvExecutionID            = "KANDER_EXECUTION_ID"
	EnvCoordinatorExecutionID = "KANDER_COORDINATOR_EXECUTION_ID"
	EnvClientHome             = "KANDER_CLIENT_HOME"
)

// ErrNotConfigured reports a client without server.url or project.name.
var ErrNotConfigured = errors.New("server.url and project.name are not configured")

// Enabled reports whether cfg selects the server mode.
func Enabled(cfg *config.Config) bool { return cfg != nil && cfg.Server.URL != "" }

// Session is one opened project on one server.
type Session struct {
	ServerURL string
	Client    *client.Client
	Project   ProjectService
	Info      api.Project
	Home      string
}

// Home returns the private client directory of this user.
func Home() (string, error) {
	if dir := os.Getenv(EnvClientHome); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", errors.New(EnvClientHome + " must be an absolute path")
		}
		return dir, nil
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.ShareDir, "client"), nil
}

func ensureHome(home string) error {
	if err := os.MkdirAll(filepath.Dir(home), 0o700); err != nil {
		return err
	}
	return fs.EnsurePrivateDirectory(filepath.Dir(home), home, true)
}

// ClientID returns the persistent identity of this host, creating it once.
func ClientID(home string) (string, error) {
	if err := ensureHome(home); err != nil {
		return "", err
	}
	path := filepath.Join(home, "client-id")
	data, ok, err := fs.ReadRegularFileIfExists(home, path)
	if err != nil {
		return "", err
	}
	if ok {
		id := strings.TrimSpace(string(data))
		if !api.ValidIdentity(id) {
			return "", errors.New("client identity file " + path + " is damaged")
		}
		return id, nil
	}
	host, _ := os.Hostname()
	host = sanitize(host)
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := "c-" + hex.EncodeToString(b)
	if host != "" {
		id += "-" + host
	}
	if err := fs.WriteTextAtomic(home, path, id+"\n", false); err != nil {
		// A concurrent first run may have won the exclusive create.
		if data, ok, rerr := fs.ReadRegularFileIfExists(home, path); rerr == nil && ok {
			return strings.TrimSpace(string(data)), nil
		}
		return "", err
	}
	return id, nil
}

func sanitize(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// GitRemote returns the origin URL of the repository at dir, or "" when there
// is none. It binds the project name to one repository on the server.
func GitRemote(ctx context.Context, dir string) string {
	remote, _ := ResolveGitRemote(ctx, dir)
	return remote
}

// ResolveGitRemote permits board reads outside a checkout, but refuses remote
// values that would leak credentials or ambiguously identify several origins.
func ResolveGitRemote(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "--all", "origin")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", nil
	}
	value := strings.TrimSpace(string(out))
	if !api.SafeGitRemote(value) {
		return "", errors.New(config.Text("remote.git_unsafe_origin"))
	}
	return value, nil
}

// ProjectDir returns the main worktree of the repository at dir, or dir when
// it is not in a Git repository. Agents start there, as in v1.
func ProjectDir(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err == nil {
		first, _, _ := strings.Cut(string(out), "\n")
		if main, ok := strings.CutPrefix(first, "worktree "); ok && filepath.IsAbs(main) {
			return main
		}
	}
	return dir
}

// Open resolves the project of cfg and checks protocol compatibility.
func Open(ctx context.Context, cfg *config.Config, dir string) (*Session, error) {
	if !Enabled(cfg) || cfg.Project.Name == "" {
		return nil, ErrNotConfigured
	}
	home, err := Home()
	if err != nil {
		return nil, err
	}
	id, err := ClientID(home)
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg.Server.URL, id)
	if err != nil {
		return nil, err
	}
	if _, err := c.CheckCompatible(ctx); err != nil {
		return nil, err
	}
	repository, err := ResolveGitRemote(ctx, dir)
	if err != nil {
		return nil, err
	}
	info, err := c.Register(ctx, cfg.Project.Name, repository)
	if err != nil {
		return nil, err
	}
	return &Session{ServerURL: cfg.Server.URL, Client: c, Project: c.Project(info.ID), Info: info, Home: home}, nil
}

// Credential is one execution's claim on this host. The worker renews it
// until the server releases the claim or the lease is lost; a lost credential
// is kept, marked, so its worktree and branch stay for manual handling.
type Credential struct {
	Worktree      string       `json:"worktree,omitempty"`
	Branch        string       `json:"branch,omitempty"`
	BaseCommit    string       `json:"base_commit,omitempty"`
	ResourceKind  string       `json:"resource_kind,omitempty"`
	ServerURL     string       `json:"server_url"`
	ClientID      string       `json:"client_id"`
	ProjectID     string       `json:"project_id"`
	TaskID        string       `json:"task_id"`
	ExecutionID   string       `json:"execution_id"`
	Generation    uint64       `json:"generation"`
	Token         string       `json:"token"`
	RenewInterval api.Duration `json:"renew_interval"`
	TTL           api.Duration `json:"ttl"`
	RenewedAt     time.Time    `json:"renewed_at"`
	// AgentPID is the managed agent process the worker stops when the lease
	// is lost; zero when the claim was taken by hand.
	AgentPID     int      `json:"agent_pid,omitzero"`
	AgentStart   string   `json:"agent_start,omitempty"`
	BootID       string   `json:"boot_id,omitempty"`
	Window       string   `json:"window,omitempty"`
	Session      string   `json:"session,omitempty"`
	StopAttempts []string `json:"stop_attempts,omitempty"`
	StopResult   string   `json:"stop_result,omitempty"`
	Lost         bool     `json:"lost,omitzero"`
	// Delivered lists the collaboration requests the worker already typed
	// into the agent.
	Delivered  []string `json:"delivered,omitempty"`
	LostReason string   `json:"lost_reason,omitempty"`
}

// Claim returns the headers form of the credential.
func (c Credential) Claim() *client.Claim {
	return &client.Claim{Token: c.Token, Generation: c.Generation}
}

func credentialDir(home, project, task string) string {
	return filepath.Join(home, "executions", project, task)
}

// Credentials lists every credential record below home.
func Credentials(home string) ([]Credential, error) {
	root := filepath.Join(home, "executions")
	exists, err := fs.DirectoryExists(home, root)
	if err != nil || !exists {
		return nil, err
	}
	var out []Credential
	projects, err := fs.ListDirectory(home, root)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.Kind != fs.KindDirectory {
			continue
		}
		tasks, err := fs.ListDirectory(home, filepath.Join(root, p.Name))
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			if t.Kind != fs.KindDirectory {
				continue
			}
			dir := filepath.Join(root, p.Name, t.Name)
			files, err := fs.ListDirectory(home, dir)
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				if f.Kind != fs.KindFile || !strings.HasSuffix(f.Name, ".json") {
					continue
				}
				data, err := fs.ReadRegularFile(home, filepath.Join(dir, f.Name))
				if err != nil {
					return nil, err
				}
				released, err := fs.RegularFileExists(home, filepath.Join(dir, strings.TrimSuffix(f.Name, ".json")+".released"))
				if err != nil {
					return nil, err
				}
				if released {
					continue
				}
				var c Credential
				if err := json.Unmarshal(data, &c); err != nil {
					return nil, errors.New("execution credential " + filepath.Join(dir, f.Name) + " is damaged")
				}
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// Credential returns the credential this process may present for task. A
// process started for one execution (EnvExecutionID set) gets only that
// execution's credential; an interactive process gets the latest claim this
// host made. found=false means this host holds no claim the process may use.
func (s *Session) Credential(task string) (Credential, bool, error) {
	return s.credentialFor(task, os.Getenv(EnvExecutionID))
}
func (s *Session) CoordinatorCredential(group string) (Credential, bool, error) {
	execution := os.Getenv(EnvCoordinatorExecutionID)
	if execution == "" {
		execution = os.Getenv(EnvExecutionID)
	}
	return s.credentialFor(group, execution)
}
func (s *Session) credentialFor(task, execution string) (Credential, bool, error) {
	var indexedGeneration uint64
	if !api.ValidIdentity(task) || !api.ValidIdentity(s.Info.ID) {
		return Credential{}, false, errors.New("invalid execution credential location")
	}
	dir := credentialDir(s.Home, s.Info.ID, task)
	if execution == "" {
		head, found, err := readCredentialHead(s.Home, dir)
		if err != nil {
			return Credential{}, false, err
		}
		if found {
			execution, indexedGeneration = head.ExecutionID, head.Generation
		} else {
			data, ok, err := fs.ReadRegularFileIfExists(s.Home, filepath.Join(dir, "latest"))
			if err != nil || !ok {
				return Credential{}, false, err
			}
			execution = strings.TrimSpace(string(data))
		}
	}
	if !api.ValidIdentity(execution) {
		return Credential{}, false, nil
	}
	released, err := fs.RegularFileExists(s.Home, filepath.Join(dir, execution+".released"))
	if err != nil || released {
		return Credential{}, false, err
	}
	data, ok, err := fs.ReadRegularFileIfExists(s.Home, filepath.Join(dir, execution+".json"))
	if err != nil || !ok {
		return Credential{}, false, err
	}
	var c Credential
	if err := json.Unmarshal(data, &c); err != nil {
		return Credential{}, false, errors.New("execution credential of " + task + " is damaged")
	}
	if indexedGeneration != 0 && c.Generation != indexedGeneration {
		return Credential{}, false, errors.New(config.Text("remote.credential_head_damaged"))
	}
	if c.ProjectID != s.Info.ID || c.TaskID != task || c.ExecutionID != execution || s.ServerURL != "" && c.ServerURL != s.ServerURL || s.Client != nil && c.ClientID != s.Client.ClientID() {
		return Credential{}, false, errors.New("execution credential identity does not match its private location")
	}
	return c, true, nil
}

// NewExecutionID returns a fresh execution identity.
func NewExecutionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "x-" + hex.EncodeToString(b)
}
