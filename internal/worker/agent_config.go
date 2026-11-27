package worker

import (
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// agentConfigDir is the directory the terminal hooks read the agent
// definitions of c with: the directory the credential records
// (remote.Credential.AgentConfigDir). A recorded directory that is not an
// existing absolute directory gives "", so the hooks read the configuration
// of the worker's own directory and never search for an overlay above the
// missing one; it is logged once per credential and directory.
func (w *loop) agentConfigDir(c remote.Credential) string {
	dir := c.AgentConfigDir()
	if dir == "" || config.UsableConfigDir(dir) {
		return dir
	}
	key := keyOf(c)
	w.mu.Lock()
	logged := w.missingDirs[key] == dir
	w.missingDirs[key] = dir
	w.mu.Unlock()
	if !logged {
		w.o.Logger.Warn("agent configuration directory is missing; using the scope configuration",
			"project", c.ProjectID, "task", c.TaskID, "dir", dir)
	}
	return ""
}
