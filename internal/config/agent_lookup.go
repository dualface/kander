package config

import (
	"os"
	"path/filepath"
	"sync"
)

// AgentLookup reads the definition of an agent by name. The zero value reads
// it with LoadAgent, from the project overlay of the process directory.
type AgentLookup func(name string) (AgentDefinition, error)

// Agent reads the definition of name through l, or through LoadAgent when l
// is nil.
func (l AgentLookup) Agent(name string) (AgentDefinition, error) {
	if l == nil {
		return LoadAgent(name)
	}
	return l(name)
}

// UsableConfigDir reports whether dir can select a project overlay: an
// absolute path of an existing directory. Any other value, the empty string
// included, keeps the configuration of the process directory.
func UsableConfigDir(dir string) bool {
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// AgentLookupIn returns a lookup that reads like LoadAgent, with the project
// overlay looked up from dir instead of the process directory. A dir that
// UsableConfigDir refuses reads what LoadAgent reads: an overlay is never
// searched for above a directory that does not exist. An invalid overlay of
// dir is an error; it never falls back to the scope configuration.
//
// The lookup reads the configuration on its first call and answers every
// later call from that read, so one lookup serves one operation: a later
// operation builds a new lookup and sees a changed overlay. It is safe for
// concurrent use.
func AgentLookupIn(dir string) AgentLookup {
	load := sync.OnceValues(func() (*Config, error) {
		if !UsableConfigDir(dir) {
			return Load(false)
		}
		return LoadFor(dir)
	})
	return func(name string) (AgentDefinition, error) {
		cfg, err := load()
		if err != nil {
			return AgentDefinition{}, err
		}
		return agentOf(cfg, name)
	}
}

// agentOf is the definition of name in a loaded configuration.
func agentOf(cfg *Config, name string) (AgentDefinition, error) {
	if !HasAgent(cfg, name) {
		return AgentDefinition{}, UnavailableAgentError(cfg, name)
	}
	return AgentFor(cfg, name), nil
}
