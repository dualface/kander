package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
)

// legacyProjectEntrySuffix ends every reference a retired project install wrote: the project
// rules files pointed at .kander/rules/KANDER-AGENTS.md of the main worktree.
const legacyProjectEntrySuffix = ".kander/rules/KANDER-AGENTS.md"

// ProjectReferenceState is the reference state of one project rules file, shared by the
// covered agents that read it.
type ProjectReferenceState struct {
	Agents []string
	Target string
	// Present reports a reference to the global rules entry where the agents load it.
	Present bool
	// Detail is the localized reason when Present is false.
	Detail string
	// Legacy reports a load command that still names the retired project install entry.
	Legacy bool
	// DisabledBy lists the agents with install_rules off that read the file; kander init does
	// not write it, so its state is not a problem.
	DisabledBy []string
}

// InspectProjectReferences reports, without writing, the state of every project rules file
// kander init would write, once per file.
func InspectProjectReferences(project ProjectRules, switches config.InstallRules) []ProjectReferenceState {
	var out []ProjectReferenceState
	index := map[string]int{}
	for _, agent := range ProjectReferenceAgents() {
		target := ProjectRulesTarget(agent, project.Root)
		key := rulesFileKey(target)
		if i, ok := index[key]; ok {
			out[i].Agents = append(out[i].Agents, agent)
			continue
		}
		index[key] = len(out)
		state := ProjectReferenceState{Agents: []string{agent}, Target: target}
		state.DisabledBy = DisabledReaders(target, project.Root, switches)
		state.Present, state.Detail = ProjectReferenceStatus(agent, project)
		state.Legacy = fileHasLegacyProjectReference(target, project.Root)
		out = append(out, state)
	}
	return out
}

// fileHasLegacyProjectReference reports whether the rules file, read inside the main worktree,
// carries a load command that names the retired project install entry.
func fileHasLegacyProjectReference(target, root string) bool {
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil || !withinDirectory(root, resolved) {
		return false
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return false
	}
	return HasLegacyProjectReference(string(data))
}

// HasLegacyProjectReference reports whether text carries a Kander load command, outside HTML
// comments and code fences, that names the retired .kander/rules/KANDER-AGENTS.md.
func HasLegacyProjectReference(text string) bool {
	for _, line := range liveLines(text) {
		for _, token := range rulesEntryTokenRE.FindAllString(line, -1) {
			if strings.HasSuffix(filepath.ToSlash(token), legacyProjectEntrySuffix) {
				return true
			}
		}
	}
	return false
}
