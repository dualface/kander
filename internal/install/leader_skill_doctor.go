package install

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/skills"
)

// LeaderSkillStatus reports what InspectLeaderSkills or EnsureLeaderSkills found at
// one leader skill directory.
type LeaderSkillStatus int

const (
	// LeaderSkillInstalled means every bundled file the skill manages matches the bundle.
	LeaderSkillInstalled LeaderSkillStatus = iota
	// LeaderSkillMissing means the directory or one of its managed files does not exist.
	LeaderSkillMissing
	// LeaderSkillOutdated means a managed file still holds the previously installed bytes.
	LeaderSkillOutdated
	// LeaderSkillModified means a managed file was edited locally; it is never overwritten.
	LeaderSkillModified
	// LeaderSkillUnmanaged means the directory or link has no receipt for this scope and
	// destination, so it belongs to the user and is left untouched.
	LeaderSkillUnmanaged
	// LeaderSkillSkipped means the destination cannot be inspected safely, for example
	// because an ancestor is a link; Reason says why.
	LeaderSkillSkipped
	// LeaderSkillWritten means EnsureLeaderSkills created a missing skill directory.
	LeaderSkillWritten
	// LeaderSkillUpdated means EnsureLeaderSkills restored or updated managed files.
	LeaderSkillUpdated
	// LeaderSkillDisabled means the agent has install_rules off; nothing was read or written.
	LeaderSkillDisabled
)

// LeaderSkillOutcome records the inspection or repair result of one leader skill directory.
type LeaderSkillOutcome struct {
	Agent  string
	Target string
	Status LeaderSkillStatus
	// Modified reports managed files kept because of local edits, also after an update.
	Modified bool
	// Reason explains LeaderSkillSkipped.
	Reason error
	// Err reports a destination that could not be resolved, inspected, or written.
	Err error
}

// InspectLeaderSkills reports, without writing, the leader skill state of every
// destination the scope's install covers.
func InspectLeaderSkills(paths config.InstallPaths, switches config.InstallRules) []LeaderSkillOutcome {
	return leaderSkillOutcomes(paths, switches, false)
}

// EnsureLeaderSkills installs a missing leader skill and updates a managed one at every
// destination the scope's install covers, with the same ownership rules as install:
// local edits, user-owned directories and links, and unsafe destinations are kept.
func EnsureLeaderSkills(paths config.InstallPaths, switches config.InstallRules) []LeaderSkillOutcome {
	return leaderSkillOutcomes(paths, switches, true)
}

func leaderSkillOutcomes(paths config.InstallPaths, switches config.InstallRules, repair bool) []LeaderSkillOutcome {
	var outcomes []LeaderSkillOutcome
	for _, target := range leaderDestinations(paths) {
		outcome := LeaderSkillOutcome{Agent: target.agent, Target: target.dir}
		switch {
		case !switches.Enabled(target.agent):
			outcome.Status = LeaderSkillDisabled
		case target.err != nil:
			outcome.Err = target.err
		default:
			outcome = checkLeaderSkill(outcome, paths, repair)
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func checkLeaderSkill(outcome LeaderSkillOutcome, paths config.InstallPaths, repair bool) LeaderSkillOutcome {
	s, err := inspectLeaderSkill(outcome.Target, paths)
	if err != nil {
		outcome.Err = err
		return outcome
	}
	if s.keepReason != nil {
		outcome.Status = LeaderSkillSkipped
		outcome.Reason = fmt.Errorf("%s: %w", s.dir, s.keepReason)
		return outcome
	}
	if s.exists && !s.managed {
		outcome.Status = LeaderSkillUnmanaged
		return outcome
	}
	outcome.Status, outcome.Modified, err = classifyLeaderSkill(s)
	if err != nil {
		outcome.Err = err
		return outcome
	}
	if !repair || (outcome.Status != LeaderSkillMissing && outcome.Status != LeaderSkillOutdated) {
		return outcome
	}
	written := LeaderSkillUpdated
	if !s.exists {
		written = LeaderSkillWritten
	}
	if err := s.install(paths); err != nil {
		outcome.Err = err
		return outcome
	}
	outcome.Status = written
	return outcome
}

// classifyLeaderSkill reads a missing or managed skill. Missing wins over Outdated, and
// either wins over a local edit, which a repair keeps while writing the other files.
func classifyLeaderSkill(s leaderSkill) (LeaderSkillStatus, bool, error) {
	if !s.exists {
		return LeaderSkillMissing, false, nil
	}
	missing, outdated, modified := false, false, false
	for _, name := range leaderFiles {
		target := filepath.Join(s.dir, filepath.FromSlash(name))
		got, found, err := fs.ReadRegularFileIfExists(s.anchor, target)
		if err != nil {
			return 0, false, err
		}
		want, err := skills.Leader.ReadFile(leaderName + "/" + name)
		if err != nil {
			return 0, false, err
		}
		switch {
		case !found:
			missing = true
		case bytes.Equal(got, want):
		case fileHash(got) == s.receipt.Files[name]:
			outdated = true
		default:
			modified = true
		}
	}
	switch {
	case missing:
		return LeaderSkillMissing, modified, nil
	case outdated:
		return LeaderSkillOutdated, modified, nil
	case modified:
		return LeaderSkillModified, true, nil
	}
	return LeaderSkillInstalled, false, nil
}
