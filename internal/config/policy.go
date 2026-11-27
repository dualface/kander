package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The project policy is the part of the configuration a server keeps once per
// project, so every client of the project works with the same values: the Git
// branch mode, the review stages, the rule module switches, the agent
// language, and the issue completion handoff. Every other key stays host
// configuration. See docs/project-policy.md.

// Top-level keys of a project policy, in display order.
const (
	PolicyKeyGit           = "git"
	PolicyKeyReviewStages  = "review_stages"
	PolicyKeyRules         = "rules"
	PolicyKeyAgentLanguage = "agent_language"
	PolicyKeyIssue         = "issue"
)

// PolicyKeys lists the top-level keys of a project policy.
var PolicyKeys = []string{PolicyKeyGit, PolicyKeyReviewStages, PolicyKeyRules, PolicyKeyAgentLanguage, PolicyKeyIssue}

// PolicyValues are the five values of a project policy. The JSON form uses the
// keys and value shapes of the configuration file.
type PolicyValues struct {
	Git           Git                          `json:"git"`
	ReviewStages  map[string]map[string]string `json:"review_stages"`
	Rules         Rules                        `json:"rules"`
	AgentLanguage string                       `json:"agent_language"`
	Issue         Issue                        `json:"issue"`
}

// PolicyValuesOf copies the policy values of cfg.
func PolicyValuesOf(cfg *Config) PolicyValues {
	return PolicyValues{
		Git:           cfg.Git,
		ReviewStages:  cloneNested(cfg.ReviewStages),
		Rules:         cfg.Rules.Clone(),
		AgentLanguage: cfg.AgentLanguage,
		Issue:         cfg.Issue,
	}
}

// ApplyTo replaces the policy values of cfg with copies of v.
func (v PolicyValues) ApplyTo(cfg *Config) {
	cfg.Git = v.Git
	cfg.ReviewStages = cloneNested(v.ReviewStages)
	cfg.Rules = v.Rules.Clone()
	cfg.AgentLanguage = v.AgentLanguage
	cfg.Issue = v.Issue
}

// PolicyLeaf is one value of a project policy and its JSON path.
type PolicyLeaf struct {
	Path  string
	Value string
}

// Leaves lists the values of v in the order of PolicyLeafPaths.
func (v PolicyValues) Leaves() []PolicyLeaf {
	paths := PolicyLeafPaths()
	leaves := make([]PolicyLeaf, 0, len(paths))
	for _, path := range paths {
		value, _ := v.leaf(path)
		leaves = append(leaves, PolicyLeaf{Path: path, Value: value})
	}
	return leaves
}

func (v PolicyValues) leaf(path string) (string, bool) {
	parts := strings.Split(path, ".")
	switch {
	case path == "git.branch_mode":
		return v.Git.BranchMode, true
	case path == "issue.result_on_done":
		return v.Issue.ResultOnDone, true
	case path == PolicyKeyAgentLanguage:
		return v.AgentLanguage, true
	case len(parts) == 2 && parts[0] == PolicyKeyRules:
		enabled, ok := v.Rules[parts[1]]
		return strconv.FormatBool(enabled), ok
	case len(parts) == 3 && parts[0] == PolicyKeyReviewStages:
		mode, ok := v.ReviewStages[parts[1]][parts[2]]
		return mode, ok
	}
	return "", false
}

// PolicyLeafPaths lists the JSON path of every value of a project policy this
// version knows: the only keys kander policy set accepts.
func PolicyLeafPaths() []string {
	paths := []string{"git.branch_mode"}
	for _, scale := range TaskScales {
		for _, role := range ReviewRoles {
			paths = append(paths, PolicyKeyReviewStages+"."+scale+"."+role)
		}
	}
	for _, module := range RuleModules {
		paths = append(paths, PolicyKeyRules+"."+module)
	}
	return append(paths, PolicyKeyAgentLanguage, "issue.result_on_done")
}

// PolicyProblems lists why a set of policy values was refused: the values that
// are missing, the keys this version does not know, and the values that fail
// the configuration rules.
type PolicyProblems struct {
	Missing []string
	Unknown []string
	Invalid []string
}

func (p *PolicyProblems) empty() bool {
	return len(p.Missing) == 0 && len(p.Unknown) == 0 && len(p.Invalid) == 0
}

func (p *PolicyProblems) Error() string {
	var parts []string
	if len(p.Missing) > 0 {
		parts = append(parts, Text("policy.values_missing", strings.Join(p.Missing, ", ")))
	}
	if len(p.Unknown) > 0 {
		parts = append(parts, Text("policy.values_unknown", strings.Join(p.Unknown, ", ")))
	}
	parts = append(parts, p.Invalid...)
	return Text("policy.values_invalid", strings.Join(parts, "; "))
}

// ValidatePolicyValues decodes the values of a policy write strictly, as the
// server does before it stores them: all five values must be present, a key
// this version does not know anywhere in the document is refused, and every
// value must pass the configuration rules. A flat review_stages object is
// normalized to the two-scale form first, and a rule module that is missing
// takes its default value. The error is a *PolicyProblems that lists every
// problem.
func ValidatePolicyValues(data []byte) (PolicyValues, error) {
	values, problems := parsePolicyValues(data)
	if !problems.empty() {
		return PolicyValues{}, problems
	}
	return values, nil
}

// ReadPolicyValues decodes policy values the way a client reads them from a
// server or from its cache: a key this version does not know is ignored and
// returned by its JSON path, so a policy a newer version wrote still applies.
// A missing or invalid value is an error.
func ReadPolicyValues(data []byte) (PolicyValues, []string, error) {
	values, problems := parsePolicyValues(data)
	ignored := problems.Unknown
	problems.Unknown = nil
	if !problems.empty() {
		return PolicyValues{}, nil, problems
	}
	return values, ignored, nil
}

func parsePolicyValues(data []byte) (PolicyValues, *PolicyProblems) {
	problems := &PolicyProblems{}
	raw, err := decodeJSON(bytes.TrimSpace(data))
	if err != nil {
		problems.Invalid = append(problems.Invalid, Text("policy.values_not_object"))
		return PolicyValues{}, problems
	}
	obj, ok := asObject(raw)
	if !ok {
		problems.Invalid = append(problems.Invalid, Text("policy.values_not_object"))
		return PolicyValues{}, problems
	}
	var values PolicyValues
	for _, key := range sortedKeys(obj) {
		if !slices.Contains(PolicyKeys, key) {
			problems.Unknown = append(problems.Unknown, key)
		}
	}
	for _, key := range PolicyKeys {
		if _, ok := obj[key]; !ok {
			problems.Missing = append(problems.Missing, key)
		}
	}
	if value, ok := obj[PolicyKeyGit]; ok {
		values.Git = Git{BranchMode: policyChoice(value, PolicyKeyGit, "branch_mode", GitBranchModes, problems)}
	}
	if value, ok := obj[PolicyKeyIssue]; ok {
		values.Issue = Issue{ResultOnDone: policyChoice(value, PolicyKeyIssue, "result_on_done", IssueResultModes, problems)}
	}
	if value, ok := obj[PolicyKeyReviewStages]; ok {
		values.ReviewStages = policyReviewStages(value, problems)
	}
	if value, ok := obj[PolicyKeyRules]; ok {
		values.Rules = policyRules(value, problems)
	}
	if value, ok := obj[PolicyKeyAgentLanguage]; ok {
		language, err := validateAgentLanguage(value)
		if err != nil {
			problems.Invalid = append(problems.Invalid, err.Error())
		}
		values.AgentLanguage = language
	}
	return values, problems
}

// policyChoice reads the one key of a policy section such as git or issue.
func policyChoice(raw any, section, key string, choices []string, problems *PolicyProblems) string {
	obj, ok := asObject(raw)
	if !ok {
		problems.Invalid = append(problems.Invalid, Text("policy.section_not_object", section))
		return ""
	}
	for _, name := range sortedKeys(obj) {
		if name != key {
			problems.Unknown = append(problems.Unknown, section+"."+name)
		}
	}
	value, exists := obj[key]
	if !exists {
		problems.Missing = append(problems.Missing, section+"."+key)
		return ""
	}
	mode, err := validateChoice(value, choices, section+"."+key)
	if err != nil {
		problems.Invalid = append(problems.Invalid, err.Error())
	}
	return mode
}

func policyReviewStages(raw any, problems *PolicyProblems) map[string]map[string]string {
	// Keys that are neither a scale nor a role are listed first, so a section whose shape is
	// refused below still names them.
	if obj, ok := asObject(raw); ok {
		for _, key := range sortedKeys(obj) {
			if !slices.Contains(TaskScales, key) && !containsKey(historicalReviewRoleSet(), key) {
				problems.Unknown = append(problems.Unknown, PolicyKeyReviewStages+"."+key)
			}
		}
	}
	normalized, err := NormalizeReviewStages(raw)
	if err != nil {
		problems.Invalid = append(problems.Invalid, err.Error())
		return nil
	}
	stages := map[string]map[string]string{}
	for _, scale := range TaskScales {
		path := PolicyKeyReviewStages + "." + scale
		value, exists := normalized[scale]
		if !exists {
			problems.Missing = append(problems.Missing, path)
			continue
		}
		roles, ok := asObject(value)
		if !ok {
			problems.Invalid = append(problems.Invalid, Text("policy.section_not_object", path))
			continue
		}
		for _, key := range sortedKeys(roles) {
			if !slices.Contains(ReviewRoles, key) {
				problems.Unknown = append(problems.Unknown, path+"."+key)
			}
		}
		stages[scale] = map[string]string{}
		for _, role := range ReviewRoles {
			mode, exists := roles[role]
			if !exists {
				problems.Missing = append(problems.Missing, path+"."+role)
				continue
			}
			valid, err := validateChoice(mode, ReviewStageModes, path+"."+role)
			if err != nil {
				problems.Invalid = append(problems.Invalid, err.Error())
				continue
			}
			stages[scale][role] = valid
		}
	}
	return stages
}

// policyRules reads the rule switches. A module this version knows and the
// values leave out takes its default: a policy that an older version wrote
// does not name the modules added since.
func policyRules(raw any, problems *PolicyProblems) Rules {
	obj, ok := asObject(raw)
	if !ok {
		problems.Invalid = append(problems.Invalid, Text("rules.object_required"))
		return nil
	}
	rules := DefaultRules(true)
	for _, key := range sortedKeys(obj) {
		if !contains(RuleModules, key) {
			problems.Unknown = append(problems.Unknown, PolicyKeyRules+"."+key)
			continue
		}
		enabled, ok := obj[key].(bool)
		if !ok {
			problems.Invalid = append(problems.Invalid, Text("rules.boolean_required", key))
			continue
		}
		rules[key] = enabled
	}
	if err := ValidateRules(rules); err != nil {
		problems.Invalid = append(problems.Invalid, err.Error())
	}
	return rules
}

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// PolicyOverride is one value of a configuration file that the project policy
// replaces: its JSON path, the file, the value in the file, and the policy
// value.
type PolicyOverride struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Local  string `json:"local"`
	Policy string `json:"policy"`
}

// policyOverrides lists the values of the given layers that differ from the
// policy. A layer is a decoded configuration document and the file it came
// from; only the keys the document holds count, so a default never shows as
// overridden.
func policyOverrides(values PolicyValues, layers []policyLayer) []PolicyOverride {
	var overrides []PolicyOverride
	for _, layer := range layers {
		if layer.doc == nil {
			continue
		}
		doc := cloneRawObjectDeep(layer.doc)
		if normalizeReviewStagesField(doc) != nil {
			continue
		}
		FoldLegacyReviewRoleKeys(doc)
		for _, leaf := range values.Leaves() {
			local, ok := rawLeaf(doc, strings.Split(leaf.Path, "."))
			if ok && local != leaf.Value {
				overrides = append(overrides, PolicyOverride{Path: leaf.Path, Source: layer.path, Local: local, Policy: leaf.Value})
			}
		}
	}
	return overrides
}

type policyLayer struct {
	path string
	doc  map[string]any
}

func rawLeaf(doc map[string]any, path []string) (string, bool) {
	var current any = doc
	for _, part := range path {
		obj, ok := asObject(current)
		if !ok {
			return "", false
		}
		if current, ok = obj[part]; !ok {
			return "", false
		}
	}
	switch value := current.(type) {
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case json.Number:
		return value.String(), true
	}
	data, err := json.Marshal(current)
	if err != nil {
		return fmt.Sprint(current), true
	}
	return string(data), true
}
