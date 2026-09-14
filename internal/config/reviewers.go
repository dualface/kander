package config

import "strings"

func defaultReviewerRoles(agent string) map[string]string {
	out := make(map[string]string, len(ReviewRoles))
	for _, role := range ReviewRoles {
		out[role] = agent
	}
	return out
}

// DefaultReviewers returns both task scales with every role pointing at the default agent.
func DefaultReviewers() map[string]map[string]string {
	agent := defaultAgentName()
	out := make(map[string]map[string]string, len(TaskScales))
	for _, scale := range TaskScales {
		out[scale] = defaultReviewerRoles(agent)
	}
	return out
}

// NormalizeReviewers rewrites a raw reviewers JSON value into the two-scale object form.
// A legacy flat {role: agent} object is copied onto both scales so a later deep-merge
// cannot mix the two shapes. Mixed scale and role keys at the same level are rejected.
func NormalizeReviewers(raw any) (map[string]any, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.reviewers_must_be_a_json_object")
	}
	scales, roles, unknown := classifyReviewStages(obj)
	if len(scales) > 0 && len(roles) > 0 {
		conflict := append(append([]string{}, scales...), roles...)
		return nil, configErrorf(
			"config.reviewers_mixes_scale_and_role_keys", strings.Join(sorted(conflict), ", "),
		)
	}
	if len(roles) > 0 || (len(scales) == 0 && reviewStagesLooksFlat(obj)) {
		if len(unknown) > 0 {
			return nil, configErrorf(
				"config.reviewers_has_unknown_roles", strings.Join(unknown, ", "),
			)
		}
		flat := cloneRawObject(obj)
		out := make(map[string]any, len(TaskScales))
		for _, scale := range TaskScales {
			out[scale] = cloneRawObject(flat)
		}
		return out, nil
	}
	if len(unknown) > 0 {
		return nil, configErrorf(
			"config.reviewers_has_unknown_scales", strings.Join(unknown, ", "), strings.Join(TaskScales, ", "),
		)
	}
	return cloneRawObject(obj), nil
}

func validateReviewerRoles(raw any, path string, reviewable *Config, reviewNames []string) (map[string]string, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.reviewers_scale_must_be_a_json_object", path)
	}
	allowed := reviewRoleSet()
	var unknown []string
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return nil, configErrorf(
			"config.reviewers_has_unknown_roles", strings.Join(unknown, ", "),
		)
	}
	reviewers := defaultReviewerRoles(defaultAgentName())
	for _, role := range ReviewRoles {
		if _, exists := obj[role]; !exists {
			continue
		}
		agent, err := validateReviewerChoice(obj[role], reviewable, path+"."+role, reviewNames)
		if err != nil {
			return nil, err
		}
		reviewers[role] = agent
	}
	return reviewers, nil
}

func validateReviewers(raw any, reviewable *Config, reviewNames []string) (map[string]map[string]string, error) {
	normalized, err := NormalizeReviewers(raw)
	if err != nil {
		return nil, err
	}
	reviewers := DefaultReviewers()
	for _, scale := range TaskScales {
		provided, exists := normalized[scale]
		if !exists {
			continue
		}
		roles, err := validateReviewerRoles(provided, "reviewers."+scale, reviewable, reviewNames)
		if err != nil {
			return nil, err
		}
		reviewers[scale] = roles
	}
	return reviewers, nil
}

// ReviewerFor picks the configured reviewer agent for one role at one task scale.
// A missing scale or role falls back to the default agent name.
func ReviewerFor(cfg *Config, scale, role string) string {
	fallback := defaultAgentName()
	if cfg == nil || cfg.Reviewers == nil {
		return fallback
	}
	if !contains(TaskScales, scale) || !contains(ReviewRoles, role) {
		return fallback
	}
	if cfg.Reviewers[scale] == nil {
		return fallback
	}
	agent := cfg.Reviewers[scale][role]
	if agent == "" {
		return fallback
	}
	return agent
}

func normalizeReviewersField(obj map[string]any) error {
	raw, exists := obj["reviewers"]
	if !exists {
		return nil
	}
	normalized, err := NormalizeReviewers(raw)
	if err != nil {
		return err
	}
	obj["reviewers"] = normalized
	return nil
}
