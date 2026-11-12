// Package server is kander-server: the single-instance board server of the
// distributed architecture. It owns every board below its data directory,
// decides task ownership with leases, and serves the /api/v1 contract of
// internal/api. It runs no Git, stores no code, and offers no generic file or
// command interface; see docs/distributed-server.md.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/fs"
)

// Defaults of the lease protocol; the server decides time.
const (
	DefaultTTL           = 90 * time.Second
	DefaultRenewInterval = 15 * time.Second
)

// Options configure one server instance.
type Options struct {
	maintenance   *maintenanceRequest
	DataDir       string
	TTL           time.Duration
	RenewInterval time.Duration
	Logger        *slog.Logger
	// Now overrides the clock in tests.
	Now     func() time.Time
	Version string
}

// Server is one opened data directory. Open takes its exclusive lock; Close
// releases it.
type Server struct {
	maintenance        *maintenanceRequest
	maintenanceChanged int
	importFault        func(string) error
	data               string
	ttl                time.Duration
	renew              time.Duration
	log                *slog.Logger
	clock              func() time.Time
	version            string
	secret             []byte
	lock               *fs.ExclusiveLock
	lockFile           *os.File
	ready              atomic.Bool

	mu       sync.Mutex
	projects map[string]*project
	byName   map[string]string
}

func (s *Server) now() time.Time { return s.clock().UTC() }

// Open locks the data directory against a second instance, then recovers
// every project before reporting ready.
func Open(o Options) (*Server, error) {
	if !filepath.IsAbs(o.DataDir) {
		return nil, errors.New("server data directory must be an absolute path")
	}
	s := &Server{
		maintenance: o.maintenance, data: o.DataDir, ttl: o.TTL, renew: o.RenewInterval, log: o.Logger, clock: o.Now, version: o.Version,
		projects: map[string]*project{}, byName: map[string]string{},
	}
	if s.ttl <= 0 {
		s.ttl = DefaultTTL
	}
	if s.renew <= 0 {
		s.renew = DefaultRenewInterval
	}
	if s.renew >= s.ttl {
		return nil, errors.New("renew interval must be shorter than the lease TTL")
	}
	if s.log == nil {
		s.log = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	if s.clock == nil {
		s.clock = time.Now
	}
	if err := os.MkdirAll(s.data, 0o700); err != nil {
		return nil, err
	}
	if err := fs.EnsurePrivateDirectory(filepath.Dir(s.data), s.data, false); err != nil {
		return nil, err
	}
	file, err := fs.OpenLockFile(s.data, filepath.Join(s.data, "server.lock"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	lock, err := fs.LockExclusiveContext(ctx, file)
	cancel()
	if err != nil {
		file.Close()
		return nil, errors.New("server data directory " + s.data + " is in use by another kander-server")
	}
	s.lock, s.lockFile = lock, file
	if err := s.load(); err != nil {
		s.Close()
		return nil, err
	}
	if s.maintenance != nil {
		if _, err := s.projectByName(s.maintenance.Project); err != nil {
			_ = s.Close()
			return nil, err
		}
		s.log.Info("offline maintenance complete", "project_name", s.maintenance.Project, "changed_tasks", s.maintenanceChanged)
	} else {
		s.ready.Store(true)
		s.log.Info("server ready", "data", s.data, "projects", len(s.projects))
	}
	return s, nil
}

// Close releases the data directory lock.
func (s *Server) Close() error {
	s.ready.Store(false)
	var err error
	if s.lock != nil {
		err = s.lock.Unlock()
		s.lock = nil
	}
	if s.lockFile != nil {
		err = errors.Join(err, s.lockFile.Close())
		s.lockFile = nil
	}
	return err
}

type registry struct {
	Projects []api.Project `json:"projects"`
}

func (s *Server) load() error {
	var secret struct {
		Secret string `json:"secret"`
	}
	found, err := readJSON(s.data, "secret.json", &secret)
	if err != nil {
		return err
	}
	if !found {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		secret.Secret = hex.EncodeToString(b)
		if err := writeJSON(s.data, "secret.json", secret); err != nil {
			return err
		}
	}
	if s.secret, err = hex.DecodeString(secret.Secret); err != nil || len(s.secret) < 32 {
		return errors.New("server secret is damaged")
	}
	var reg registry
	if _, err := readJSON(s.data, "projects.json", &reg); err != nil {
		return err
	}
	for _, info := range reg.Projects {
		if err := s.attach(info); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) attach(info api.Project) error {
	root := filepath.Join(s.data, "projects", info.ID)
	if err := ensureDir(s.data, filepath.Join("projects", info.ID)); err != nil {
		return err
	}
	p := &project{server: s, info: info, root: root, board: filepath.Join(root, "board")}
	if err := p.open(); err != nil {
		return err
	}
	s.projects[info.ID] = p
	s.byName[info.Name] = info.ID
	return nil
}

// register returns the project of name, creating it on first use. The remote
// of the first registration binds the name; a different remote is refused.
func (s *Server) register(req api.RegisterProject) (api.Project, bool, error) {
	if req.Remote != "" && !api.SafeGitRemote(req.Remote) {
		return api.Project{}, false, apiError(api.CodeInvalidRequest, "Git remote must not contain credentials or query parameters")
	}
	if !api.ValidProjectName(req.Name) {
		return api.Project{}, false, apiError(api.CodeInvalidRequest, "invalid project name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byName[req.Name]; ok {
		info := s.projects[id].info
		if req.Remote != "" && info.Remote != "" && info.Remote != req.Remote {
			return api.Project{}, false, apiError(api.CodeProjectMismatch, "project "+req.Name+" is bound to another Git remote")
		}
		return info, false, nil
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return api.Project{}, false, err
	}
	info := api.Project{ID: "p-" + hex.EncodeToString(b), Name: req.Name, Remote: req.Remote, CreatedAt: s.now()}
	if err := s.attach(info); err != nil {
		return api.Project{}, false, err
	}
	reg := registry{}
	for _, p := range s.projects {
		reg.Projects = append(reg.Projects, p.info)
	}
	slices.SortFunc(reg.Projects, func(a, b api.Project) int { return a.CreatedAt.Compare(b.CreatedAt) })
	if err := writeJSON(s.data, "projects.json", reg); err != nil {
		delete(s.projects, info.ID)
		delete(s.byName, info.Name)
		return api.Project{}, false, err
	}
	s.log.Info("project registered", "project", info.ID, "name", info.Name)
	return info, true, nil
}

func (s *Server) project(id string) (*project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, apiError(api.CodeNotFound, "project "+id+" does not exist")
	}
	return p, nil
}

func (s *Server) projectByName(name string) (api.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byName[name]
	if !ok {
		return api.Project{}, apiError(api.CodeNotFound, "project "+name+" is not registered")
	}
	return s.projects[id].info, nil
}
