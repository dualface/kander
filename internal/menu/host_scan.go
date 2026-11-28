package menu

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminal"
)

// projectScanProblem is a registered project the scan could not read the
// configuration of; its scan lists nothing.
type projectScanProblem struct {
	Project, Root, Error string
}

// scanHost scans this host for every project registered in host.json of home
// and stores the result there: per project, the agents of its effective
// configuration whose executable resolves (with the values of their
// config_home.env variables), the launchers a process without a terminal can
// start, the PATH the scan searched, and the time.
func scanHost(home string, now time.Time) (remote.HostScan, []projectScanProblem, error) {
	record, err := remote.ReadHostRecord(home)
	if err != nil {
		return remote.HostScan{}, nil, err
	}
	scan := remote.HostScan{ScannedAt: now.UTC(), Projects: []remote.ProjectScan{}}
	if len(record.Projects) == 0 {
		// A host without a registered project writes no host.json.
		return scan, nil, nil
	}
	scan = remote.HostScan{ScannedAt: now.UTC(), Path: os.Getenv("PATH"), Projects: []remote.ProjectScan{}}
	launchers := terminal.BackgroundLaunchers(runtime.GOOS == "windows", os.Getenv, exec.LookPath)
	if launchers == nil {
		launchers = []string{}
	}
	var problems []projectScanProblem
	for _, reg := range record.Projects {
		entry := remote.ProjectScan{ServerURL: reg.ServerURL, Project: reg.Project, Agents: []remote.AgentScan{}, Launchers: launchers}
		cfg, err := config.LoadFor(reg.Root)
		if err != nil {
			problems = append(problems, projectScanProblem{Project: reg.Project, Root: reg.Root, Error: err.Error()})
			entry.Launchers = []string{}
			scan.Projects = append(scan.Projects, entry)
			continue
		}
		entry.Agents = scanAgents(cfg)
		scan.Projects = append(scan.Projects, entry)
	}
	if err := remote.StoreHostScan(home, scan); err != nil {
		return remote.HostScan{}, nil, err
	}
	return scan, problems, nil
}

// scanAgents resolves the executable of every agent of cfg. An agent records
// only the config_home.env variables of its definition that are set.
func scanAgents(cfg *config.Config) []remote.AgentScan {
	agents := []remote.AgentScan{}
	for _, name := range config.AgentNames(cfg) {
		program := process.ResolveAgentProgram(config.AgentPath(cfg, name))
		if program == nil {
			continue
		}
		path, err := filepath.Abs(program.Path)
		if err != nil {
			continue
		}
		agent := remote.AgentScan{Name: name, Path: path}
		if spec, ok := config.RulesSpec(name); ok {
			for _, variable := range spec.ConfigEnv {
				if value := os.Getenv(variable.Name); value != "" {
					if agent.Env == nil {
						agent.Env = map[string]string{}
					}
					agent.Env[variable.Name] = value
				}
			}
		}
		agents = append(agents, agent)
	}
	return agents
}

// RefreshHostScan scans this host again after a configuration change, when it
// has a project registered. A failure only skips the refresh: the next
// kander doctor scans again.
func RefreshHostScan() {
	home, err := remote.Home()
	if err != nil {
		return
	}
	record, err := remote.ReadHostRecord(home)
	if err != nil || len(record.Projects) == 0 {
		return
	}
	_, _, _ = scanHost(home, time.Now())
}

// reportHost registers the bound checkout of the current directory when it
// has no registration yet, scans this host, and reports the host conflicts the
// servers of the registered projects see. It is unhealthy when the scan or a
// registration failed, or a server reports a conflict of this host.
func reportHost() bool {
	home, err := remote.Home()
	if err != nil {
		warning(err.Error())
		return false
	}
	healthy := true
	hint(config.Text("host.doctor_heading"))
	if cfg, err := config.Load(false); err == nil && cfg.Project.Name != "" {
		healthy = registerBoundCheckout(home, cfg) && healthy
	}
	scan, problems, err := scanHost(home, time.Now())
	if err != nil {
		warning(config.Text("host.scan_failed", err.Error()))
		return false
	}
	if len(scan.Projects) == 0 {
		hint(config.Text("host.scan_no_projects"))
		return healthy
	}
	for _, problem := range problems {
		healthy = false
		warning(config.Text("host.scan_project_failed", problem.Project, problem.Root, problem.Error))
	}
	for _, p := range scan.Projects {
		names := make([]string, 0, len(p.Agents))
		for _, agent := range p.Agents {
			names = append(names, agent.Name)
		}
		success(config.Text("host.scan_project", p.Project, p.ServerURL, listText(names), listText(p.Launchers)))
	}
	record, err := remote.ReadHostRecord(home)
	if err != nil {
		warning(err.Error())
		return false
	}
	return reportHostConflicts(home, record) && healthy
}

func listText(names []string) string {
	if len(names) == 0 {
		return config.Text("host.none")
	}
	return strings.Join(names, ", ")
}

// registerBoundCheckout adds the registration of the bound checkout of the
// current directory. An existing registration is never replaced: when it names
// another directory, both are reported.
func registerBoundCheckout(home string, cfg *config.Config) bool {
	cwd, err := os.Getwd()
	if err != nil {
		warning(err.Error())
		return false
	}
	root, ok := remote.RegistrationRoot(cwd)
	if !ok {
		return true
	}
	registered, added, err := remote.RegisterProjectIfMissing(home, config.ServerURL(cfg), cfg.Project.Name, root, time.Now())
	switch {
	case err != nil:
		warning(config.Text("host.registration_failed", err.Error()))
		return false
	case added:
		success(config.Text("host.registration_added", cfg.Project.Name, root))
	case registered != root:
		warning(config.Text("host.registration_elsewhere", cfg.Project.Name, registered, root))
	default:
		success(config.Text("host.registration_present", cfg.Project.Name, root))
	}
	return true
}

// reportHostConflicts asks each server of the registered projects for its
// hosts: another client ID with the name of this host, or two workers of this
// client ID, is a conflict. A server that does not answer is reported and does
// not count as a conflict.
func reportHostConflicts(home string, record remote.HostRecord) bool {
	id, err := remote.ClientID(home)
	if err != nil {
		warning(err.Error())
		return false
	}
	name, err := remote.HostName(home)
	if err != nil {
		warning(err.Error())
		return false
	}
	healthy := true
	for _, url := range record.ServerURLs() {
		c, err := client.New(url, id)
		if err != nil {
			warning(err.Error())
			healthy = false
			continue
		}
		c.Retries = 0
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		list, err := c.Hosts(ctx)
		cancel()
		if err != nil {
			hint(config.Text("host.hosts_unavailable", url, err.Error()))
			continue
		}
		self := slices.IndexFunc(list.Hosts, func(h api.Host) bool { return h.ClientID == id })
		for _, h := range list.Hosts {
			if h.Name == name && h.ClientID != id {
				healthy = false
				warning(config.Text("host.name_conflict", url, name, h.ClientID))
			}
		}
		switch {
		case self < 0:
			hint(config.Text("host.not_reported", url, name))
		case list.Hosts[self].Conflict == api.ConflictClientID:
			healthy = false
			warning(config.Text("host.client_id_conflict", url, id))
		default:
			success(config.Text("host.reported", url, name))
		}
	}
	return healthy
}
