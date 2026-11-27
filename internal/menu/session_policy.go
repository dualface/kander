package menu

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/dualface/kander/v2/internal/config"
)

// sessionPolicy is the project policy the Options panel shows and edits. While
// the policy is set, the five policy values the panel shows come from the
// server, an edit changes only edited, and saving sends edited to the server
// instead of writing it to a configuration file. base is the policy the edits
// started from: its revision is the expected revision of the next write, and a
// later refresh does not move it while edits are pending.
type sessionPolicy struct {
	// bound is set once the session belongs to a server project.
	bound      bool
	base       config.PolicySnapshot
	baseValues config.PolicyValues
	edited     config.PolicyValues
	// newer is the revision the server answered while edits were pending, 0
	// when there is none.
	newer uint64
}

// AttachPolicy gives the session the project policy of its server project. A
// session without unsaved policy edits shows the policy at once. While policy
// edits are pending the edits and their base stay as they are, and it reports
// whether the server answered a newer policy version than that base.
func (s *Session) AttachPolicy(snapshot config.PolicySnapshot) (changedUnderEdits bool) {
	s.policy.bound = true
	if s.PolicyDirty() {
		if snapshot.Epoch != s.policy.base.Epoch || snapshot.Revision > s.policy.base.Revision {
			s.policy.newer = snapshot.Revision
			return true
		}
		return false
	}
	s.ResetPolicy(snapshot)
	return false
}

// ResetPolicy shows snapshot and drops the policy edits, after a write the
// server refused because the policy changed. A policy this version cannot read
// leaves the panel on the file values, as an unset policy does.
func (s *Session) ResetPolicy(snapshot config.PolicySnapshot) {
	s.policy.bound = true
	s.policy.newer = 0
	wasBacked := s.PolicyBacked()
	s.policy.base = config.PolicySnapshot{}
	s.policy.baseValues, s.policy.edited = config.PolicyValues{}, config.PolicyValues{}
	if wasBacked {
		// Put the file values back before another policy (or none) replaces them.
		s.restoreFileValues()
	}
	if snapshot.Revision == 0 {
		return
	}
	values, _, err := config.ReadPolicyValues(snapshot.Values)
	if err != nil {
		return
	}
	s.policy.base, s.policy.baseValues = snapshot, values
	s.policy.edited = clonePolicyValues(values)
	s.showPolicy()
}

// AcceptPolicyWrite records the answer of a successful policy write: the
// written policy becomes the base, and the panel shows it.
func (s *Session) AcceptPolicyWrite(written config.PolicySnapshot) {
	s.ResetPolicy(written)
}

// PolicyBacked reports whether the five policy values of the panel come from
// the server: the project has a policy this version can read.
func (s *Session) PolicyBacked() bool {
	return s != nil && s.policy.base.Revision > 0
}

// PolicyRevision is the revision the policy edits started from, the expected
// revision of the next write; 0 when no policy applies.
func (s *Session) PolicyRevision() uint64 {
	if s == nil {
		return 0
	}
	return s.policy.base.Revision
}

// PolicyDirty reports whether the policy values differ from the policy the
// edits started from.
func (s *Session) PolicyDirty() bool {
	if !s.PolicyBacked() {
		return false
	}
	// Compare the encodings: an edit may hold an empty map where the base has none.
	edited, editedErr := json.Marshal(s.policy.edited)
	base, baseErr := json.Marshal(s.policy.baseValues)
	return editedErr != nil || baseErr != nil || !bytes.Equal(edited, base)
}

// PolicyChangedOnServer is the newer revision the server answered while policy
// edits were pending; 0 when there is none.
func (s *Session) PolicyChangedOnServer() uint64 {
	if s == nil {
		return 0
	}
	return s.policy.newer
}

// PolicyWrite is the next policy write: the expected revision and the edited
// values laid over the values the server answered, so the keys this version
// does not know stay as they are.
func (s *Session) PolicyWrite() (uint64, json.RawMessage, error) {
	values, err := config.MergePolicyValues(s.policy.base.Values, s.policy.edited)
	return s.policy.base.Revision, values, err
}

// PolicyLabel is the note an Options field shows after its title when path
// names a policy value of a server project: the policy revision the value
// comes from, or, while the project has no policy, how to set one. It is empty
// for every other field.
func (s *Session) PolicyLabel(path ...string) string {
	if s == nil || !s.policy.bound || !isPolicyPath(path) {
		return ""
	}
	if s.PolicyBacked() {
		return config.Text("tui.policy_source", strconv.FormatUint(s.policy.base.Revision, 10))
	}
	return config.Text("tui.policy_unset_hint")
}

func isPolicyPath(path []string) bool {
	if len(path) == 0 {
		return false
	}
	switch path[0] {
	case config.PolicyKeyAgentLanguage, config.PolicyKeyRules, config.PolicyKeyReviewStages:
		return true
	case config.PolicyKeyGit:
		return len(path) == 2 && path[1] == "branch_mode"
	case config.PolicyKeyIssue:
		return len(path) == 2 && path[1] == "result_on_done"
	}
	return false
}

// policyEdit applies an edit of a policy value. While the policy applies, the
// edit changes only the values shown and the policy edits, never a
// configuration file buffer; it reports whether it handled the edit.
func (s *Session) policyEdit(apply func(*config.Config)) bool {
	if !s.PolicyBacked() {
		return false
	}
	apply(s.Config)
	s.policy.edited = config.PolicyValuesOf(s.Config)
	return true
}

// setConfig replaces the edit buffer and shows the policy values in it.
func (s *Session) setConfig(cfg *config.Config) {
	s.Config = cfg
	s.showPolicy()
}

// showPolicy lays the policy values over the buffer the panel shows. The scope
// buffer gets them too; saveScope puts the file values back before it writes.
func (s *Session) showPolicy() {
	if s.PolicyBacked() && s.Config != nil {
		s.policy.edited.ApplyTo(s.Config)
	}
}

// restoreFileValues puts the configuration file values of the policy back into
// the buffers that showed the policy.
func (s *Session) restoreFileValues() {
	if s.scopeConfig != nil {
		s.scopeFileValues().ApplyTo(s.scopeConfig)
	}
	if s.EditingOverlay() {
		_ = s.rebuildOverlayConfig()
	} else if s.Config != nil {
		s.Config = s.scopeBuffer()
	}
}

// scopeFileValues are the policy values of the scope configuration file.
func (s *Session) scopeFileValues() config.PolicyValues {
	baseline := s.scopeExisting
	if baseline == nil {
		baseline = s.existing
	}
	if baseline == nil {
		return config.PolicyValuesOf(config.DefaultConfig())
	}
	return config.PolicyValuesOf(baseline)
}

func clonePolicyValues(v config.PolicyValues) config.PolicyValues {
	cfg := &config.Config{}
	v.ApplyTo(cfg)
	return config.PolicyValuesOf(cfg)
}

// setReviewStage sets one review stage of cfg, creating the maps it needs.
func setReviewStage(cfg *config.Config, scale, role, mode string) {
	if cfg.ReviewStages == nil {
		cfg.ReviewStages = map[string]map[string]string{}
	}
	if cfg.ReviewStages[scale] == nil {
		cfg.ReviewStages[scale] = map[string]string{}
	}
	cfg.ReviewStages[scale][role] = mode
}
