package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// gcUnidentifiedClient is the client identity a gc reads with on a host that
// has none of its own. It cannot be a generated identity, which is hex after
// "c-"; it is never stored and only sent with reads.
const gcUnidentifiedClient = "c-gc-unidentified"

// openGC opens the bound project for `kander gc` without changing anything:
// it reads the existing client identity instead of creating one, starts no
// server and no credential worker, and resolves the configured project by
// name instead of registering it. A host without a client identity reads with
// gcUnidentifiedClient: its common remote and its local server can still
// hold data to reclaim.
func openGC(ctx context.Context) (*Session, *config.Config, error) {
	cfg, err := config.Load(false)
	if err != nil {
		return nil, nil, err
	}
	home, err := Home()
	if err != nil {
		return nil, nil, err
	}
	id, err := existingClientID(home)
	if err != nil {
		return nil, nil, err
	}
	if id == "" {
		id = gcUnidentifiedClient
	}
	c, err := client.New(config.ServerURL(cfg), id)
	if err != nil {
		return nil, nil, err
	}
	if _, err := c.CheckCompatible(ctx); err != nil {
		return nil, nil, err
	}
	info, err := c.ProjectByName(ctx, cfg.Project.Name)
	if err != nil {
		return nil, nil, err
	}
	return &Session{ServerURL: config.ServerURL(cfg), Client: c, Project: c.Project(info.ID), Info: info, Home: home}, cfg, nil
}

// existingClientID reads the client identity of home, or "" when there is
// none.
func existingClientID(home string) (string, error) {
	exists, err := fs.DirectoryExists(filepath.Dir(home), home)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	path := filepath.Join(home, "client-id")
	data, found, err := fs.ReadRegularFileIfExists(home, path)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	id := strings.TrimSpace(string(data))
	if !api.ValidIdentity(id) {
		return "", errors.New("client identity file " + path + " is damaged")
	}
	return id, nil
}

// directoryProjects lists the project IDs of serverURL that the records of
// one client project directory name: the execution credentials and retained
// executions below the namespace, its request-call records, and the task
// branches of its worktree intents. Only an ID whose namespace is the directory's own counts. foreign
// says a record names the directory's project on another server.
func directoryProjects(home, serverURL, namespace string) (projects []string, foreign bool, err error) {
	found := map[string]bool{}
	note := func(server, project string) {
		if !api.ValidIdentity(project) || projectNamespace(server, project) != namespace {
			return
		}
		if server == serverURL {
			found[project] = true
		} else {
			foreign = true
		}
	}
	for _, dir := range []string{filepath.Join(home, "executions", namespace), filepath.Join(home, "retained-executions", namespace)} {
		err := eachRecord(home, dir, func(data []byte) {
			var record struct {
				ServerURL string `json:"server_url"`
				ProjectID string `json:"project_id"`
			}
			if json.Unmarshal(data, &record) == nil {
				note(record.ServerURL, record.ProjectID)
			}
		})
		if err != nil {
			return nil, false, err
		}
	}
	err = eachFile(home, filepath.Join(home, "projects", namespace, "request-calls"), func(data []byte) {
		var record struct {
			ServerURL string `json:"server_url"`
			ProjectID string `json:"project_id"`
		}
		if json.Unmarshal(data, &record) == nil {
			note(record.ServerURL, record.ProjectID)
		}
	})
	if err != nil {
		return nil, false, err
	}
	err = eachRecord(home, filepath.Join(home, "projects", namespace, "worktree-intents"), func(data []byte) {
		var intent ExecutionWorktree
		if json.Unmarshal(data, &intent) == nil {
			if rest, ok := strings.CutPrefix(intent.Branch, "kander-task/"); ok {
				project, _, _ := strings.Cut(rest, "/")
				note(serverURL, project)
			}
		}
	})
	if err != nil {
		return nil, false, err
	}
	for project := range found {
		projects = append(projects, project)
	}
	return projects, foreign, nil
}

// eachRecord calls fn with each JSON file one task directory below dir. A
// missing directory has none.
func eachRecord(home, dir string, fn func([]byte)) error {
	tasks, err := fs.ListDirectory(home, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Kind == fs.KindDirectory {
			if err := eachFile(home, filepath.Join(dir, task.Name), fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachFile calls fn with each JSON file in dir. A missing directory has none.
func eachFile(home, dir string, fn func([]byte)) error {
	entries, err := fs.ListDirectory(home, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Kind != fs.KindFile || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		data, err := fs.ReadRegularFile(home, filepath.Join(dir, entry.Name))
		if err != nil {
			return err
		}
		fn(data)
	}
	return nil
}
