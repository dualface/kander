package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// HostRecordName is the private file in the client home that registers the
// projects of this host and keeps its last scan: the agents and launchers this
// host can start a card with. Only this host reads it; its heartbeat reports
// names only, never a path, the PATH, or an environment value.
const HostRecordName = "host.json"

// hostRecordSchema is the schema of host.json. A reader ignores unknown fields,
// and an update keeps the top-level keys it does not know.
const hostRecordSchema = 1

// HostRecord is the content of host.json.
type HostRecord struct {
	Schema int `json:"schema"`
	// Version is the kander version that last completed a version refresh on
	// this host: the rules, the skill and extension, the global doctor repair,
	// and the scan of this record were written by that version.
	Version  string                `json:"version,omitempty"`
	Projects []ProjectRegistration `json:"projects"`
	Scan     *HostScan             `json:"scan,omitempty"`
	// extra keeps the top-level keys of a newer writer.
	extra map[string]json.RawMessage
}

// ProjectRegistration binds a server project to the main worktree of its
// checkout on this host: the directory a start for that project runs in.
type ProjectRegistration struct {
	ServerURL    string    `json:"server_url"`
	Project      string    `json:"project"`
	Root         string    `json:"root"`
	RegisteredAt time.Time `json:"registered_at"`
}

// HostScan is what kander doctor found usable on this host, per registered
// project, with the PATH it searched.
type HostScan struct {
	ScannedAt time.Time `json:"scanned_at"`
	Path      string    `json:"path"`
	// Env holds the config_home.env values that were set when kander doctor took the scan,
	// for every agent definition of the registered projects, resolved or not. The version
	// refresh resolves with them and writes them back unchanged, so an agent that is not
	// resolvable for one refresh gets its values back when it resolves again.
	Env      map[string]string `json:"env,omitempty"`
	Projects []ProjectScan     `json:"projects"`
}

// ProjectScan lists the usable agents and launchers of one registered project.
type ProjectScan struct {
	ServerURL string      `json:"server_url"`
	Project   string      `json:"project"`
	Agents    []AgentScan `json:"agents"`
	Launchers []string    `json:"launchers"`
}

// AgentScan is one agent whose executable resolved: its absolute path and the
// values its definition's config_home.env variables had during the scan.
type AgentScan struct {
	Name string            `json:"name"`
	Path string            `json:"path"`
	Env  map[string]string `json:"env,omitempty"`
}

// Registration returns the registration of a project on a server.
func (r HostRecord) Registration(serverURL, project string) (ProjectRegistration, bool) {
	for _, p := range r.Projects {
		if p.ServerURL == serverURL && p.Project == project {
			return p, true
		}
	}
	return ProjectRegistration{}, false
}

// ScanOf returns the scan of a registered project.
func (r HostRecord) ScanOf(serverURL, project string) (ProjectScan, bool) {
	if r.Scan == nil {
		return ProjectScan{}, false
	}
	for _, p := range r.Scan.Projects {
		if p.ServerURL == serverURL && p.Project == project {
			return p, true
		}
	}
	return ProjectScan{}, false
}

// ServerURLs lists the servers of the registered projects, in order.
func (r HostRecord) ServerURLs() []string {
	var urls []string
	for _, p := range r.Projects {
		urls = append(urls, p.ServerURL)
	}
	slices.Sort(urls)
	return slices.Compact(urls)
}

func (r HostRecord) MarshalJSON() ([]byte, error) {
	type plain HostRecord
	data, err := json.Marshal(plain(r))
	if err != nil || len(r.extra) == 0 {
		return data, err
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(data, &known); err != nil {
		return nil, err
	}
	for key, value := range r.extra {
		if _, ok := known[key]; !ok {
			known[key] = value
		}
	}
	return json.Marshal(known)
}

func (r *HostRecord) UnmarshalJSON(data []byte) error {
	type plain HostRecord
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for _, key := range []string{"schema", "version", "projects", "scan"} {
		delete(all, key)
	}
	*r = HostRecord(p)
	if len(all) > 0 {
		r.extra = all
	}
	return nil
}

// ErrHostRecordSchema reports a host.json of a schema this version does not know.
var ErrHostRecordSchema = errors.New("host.json has a schema this version does not know")

// ReadHostRecord reads host.json of home; a missing file is an empty record.
func ReadHostRecord(home string) (HostRecord, error) {
	data, exists, err := fs.ReadRegularFileIfExists(home, filepath.Join(home, HostRecordName))
	if err != nil || !exists {
		return HostRecord{Schema: hostRecordSchema}, err
	}
	var r HostRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return HostRecord{}, errors.New("host.json is damaged: " + err.Error())
	}
	if r.Schema != hostRecordSchema {
		return HostRecord{}, ErrHostRecordSchema
	}
	return r, nil
}

// UpdateHostRecord changes host.json of home under its lock: it reads the
// record, lets change edit it, and writes it back atomically and privately.
// change returning an error writes nothing.
func UpdateHostRecord(home string, change func(*HostRecord) error) error {
	if err := ensureHome(home); err != nil {
		return err
	}
	file, err := fs.OpenLockFile(home, filepath.Join(home, HostRecordName+".lock"))
	if err != nil {
		return err
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	record, err := ReadHostRecord(home)
	if err != nil {
		return err
	}
	if err := change(&record); err != nil {
		return err
	}
	record.Schema = hostRecordSchema
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return fs.WriteTextAtomic(home, filepath.Join(home, HostRecordName), string(data)+"\n", true)
}

// RegisterProject records root as the checkout of project on serverURL and
// returns the root it replaced, if another one was registered.
func RegisterProject(home, serverURL, project, root string, now time.Time) (previous string, err error) {
	err = UpdateHostRecord(home, func(r *HostRecord) error {
		previous = ""
		entry := ProjectRegistration{ServerURL: serverURL, Project: project, Root: root, RegisteredAt: now.UTC()}
		for i, p := range r.Projects {
			if p.ServerURL == serverURL && p.Project == project {
				if p.Root != root {
					previous = p.Root
				}
				r.Projects[i] = entry
				return nil
			}
		}
		r.Projects = append(r.Projects, entry)
		slices.SortFunc(r.Projects, func(a, b ProjectRegistration) int {
			return strings.Compare(a.ServerURL+"\x00"+a.Project, b.ServerURL+"\x00"+b.Project)
		})
		return nil
	})
	return previous, err
}

// RegisterProjectIfMissing records root for a project that has no registration
// yet. It returns the registered root, which is another directory when the
// project was registered elsewhere; that registration is left as it is.
func RegisterProjectIfMissing(home, serverURL, project, root string, now time.Time) (registered string, added bool, err error) {
	err = UpdateHostRecord(home, func(r *HostRecord) error {
		if existing, ok := r.Registration(serverURL, project); ok {
			registered = existing.Root
			return errUnchanged
		}
		registered, added = root, true
		r.Projects = append(r.Projects, ProjectRegistration{ServerURL: serverURL, Project: project, Root: root, RegisteredAt: now.UTC()})
		return nil
	})
	if errors.Is(err, errUnchanged) {
		err = nil
	}
	return registered, added, err
}

// errUnchanged ends an update that writes nothing.
var errUnchanged = errors.New("unchanged")

// StoreHostScan replaces the scan of host.json.
func StoreHostScan(home string, scan HostScan) error {
	return UpdateHostRecord(home, func(r *HostRecord) error {
		r.Scan = &scan
		return nil
	})
}

// RecordHostVersion records the version that completed a version refresh.
func RecordHostVersion(home, version string) error {
	return UpdateHostRecord(home, func(r *HostRecord) error {
		r.Version = version
		return nil
	})
}

// versionRefreshLockName serializes the version refresh between kander processes.
const versionRefreshLockName = "version-refresh.lock"

// TryVersionRefreshLock takes the version refresh lock of home without waiting.
// taken is false when another process holds it; release is then nil.
func TryVersionRefreshLock(home string) (release func(), taken bool, err error) {
	if err := ensureHome(home); err != nil {
		return nil, false, err
	}
	file, err := fs.OpenLockFile(home, filepath.Join(home, versionRefreshLockName))
	if err != nil {
		return nil, false, err
	}
	lock, taken, err := fs.TryLockExclusive(file)
	if err != nil || !taken {
		_ = file.Close()
		return nil, false, err
	}
	return func() {
		_ = lock.Unlock()
		_ = file.Close()
	}, true, nil
}

// HostName is the name this host reports to its servers: host.name of the scope
// configuration, else the host part of the client ID, else the client ID.
func HostName(home string) (string, error) {
	cfg, err := config.LoadScope(true)
	if err != nil {
		return "", err
	}
	if cfg.Host.Name != "" {
		return cfg.Host.Name, nil
	}
	id, err := ClientID(home)
	if err != nil {
		return "", err
	}
	if name := ClientHost(id); config.ValidHostName(name) {
		return name, nil
	}
	if config.ValidHostName(id) {
		return id, nil
	}
	return "", errors.New(config.Text("host.name_unusable", id))
}

// gitCheckout reports whether dir is inside the work tree of a Git repository.
func gitCheckout(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// RegistrationRoot is the directory a project registration of dir records: the
// main worktree of its checkout. ok is false outside a Git checkout, which is
// never registered.
func RegistrationRoot(dir string) (root string, ok bool) {
	if !gitCheckout(dir) {
		return "", false
	}
	return ProjectDir(dir), true
}
