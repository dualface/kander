package config

import "strings"

func defaultReviewStageRoles() map[string]string {
	out := make(map[string]string, len(ReviewRoles))
	for _, role := range ReviewRoles {
		out[role] = "auto"
	}
	return out
}

func DefaultReviewStages() map[string]map[string]string {
	out := make(map[string]map[string]string, len(TaskScales))
	for _, scale := range TaskScales {
		out[scale] = defaultReviewStageRoles()
	}
	return out
}

func reviewRoleSet() map[string]struct{} {
	allowed := make(map[string]struct{}, len(ReviewRoles))
	for _, role := range ReviewRoles {
		allowed[role] = struct{}{}
	}
	return allowed
}

func reviewScaleSet() map[string]struct{} {
	allowed := make(map[string]struct{}, len(TaskScales))
	for _, scale := range TaskScales {
		allowed[scale] = struct{}{}
	}
	return allowed
}

func cloneRawObject(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneRawObject(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func classifyReviewStages(obj map[string]any) (scales, roles, unknown []string) {
	scaleSet := reviewScaleSet()
	roleSet := reviewRoleSet()
	for key := range obj {
		switch {
		case containsKey(scaleSet, key):
			scales = append(scales, key)
		case containsKey(roleSet, key):
			roles = append(roles, key)
		default:
			unknown = append(unknown, key)
		}
	}
	return sorted(scales), sorted(roles), sorted(unknown)
}

func containsKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// NormalizeReviewStages rewrites a raw review_stages JSON value into the two-scale
// object form. A legacy flat {role: mode} object is copied onto both scales so a
// later deep-merge cannot mix the two shapes. Mixed scale and role keys at the
// same level are rejected with the conflicting names.
func NormalizeReviewStages(raw any) (map[string]any, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.review_stages_must_be_a_json_object")
	}
	scales, roles, unknown := classifyReviewStages(obj)
	if len(scales) > 0 && len(roles) > 0 {
		conflict := append(append([]string{}, scales...), roles...)
		return nil, configErrorf(
			"config.review_stages_mixes_scale_and_role_keys", strings.Join(sorted(conflict), ", "),
		)
	}
	if len(roles) > 0 || (len(scales) == 0 && reviewStagesLooksFlat(obj)) {
		if len(unknown) > 0 {
			return nil, configErrorf(
				"config.review_stages_has_unknown_roles", strings.Join(unknown, ", "),
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
			"config.review_stages_has_unknown_scales", strings.Join(unknown, ", "), strings.Join(TaskScales, ", "),
		)
	}
	return cloneRawObject(obj), nil
}

func reviewStagesLooksFlat(obj map[string]any) bool {
	if len(obj) == 0 {
		return false
	}
	for _, value := range obj {
		if _, ok := value.(map[string]any); ok {
			return false
		}
	}
	return true
}

func validateReviewStageRoles(raw any, path string) (map[string]string, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.review_stages_scale_must_be_a_json_object", path)
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
			"config.review_stages_has_unknown_roles", strings.Join(sorted(unknown), ", "),
		)
	}
	stages := defaultReviewStageRoles()
	for _, role := range ReviewRoles {
		if _, exists := obj[role]; !exists {
			continue
		}
		mode, err := validateChoice(obj[role], ReviewStageModes, path+"."+role)
		if err != nil {
			return nil, err
		}
		stages[role] = mode
	}
	return stages, nil
}

func validateReviewStages(raw any) (map[string]map[string]string, error) {
	normalized, err := NormalizeReviewStages(raw)
	if err != nil {
		return nil, err
	}
	stages := DefaultReviewStages()
	for _, scale := range TaskScales {
		provided, exists := normalized[scale]
		if !exists {
			continue
		}
		roles, err := validateReviewStageRoles(provided, "review_stages."+scale)
		if err != nil {
			return nil, err
		}
		stages[scale] = roles
	}
	return stages, nil
}

// ReviewStageFor picks the stage policy of one review role for one task scale.
func ReviewStageFor(cfg *Config, scale, role string) (string, error) {
	if cfg == nil || cfg.ReviewStages == nil {
		return "", configErrorf("config.unknown_task_scale", scale)
	}
	if !contains(TaskScales, scale) {
		return "", configErrorf("config.unknown_task_scale", scale)
	}
	if !contains(ReviewRoles, role) {
		return "", configErrorf("config.review_stages_has_unknown_roles", role)
	}
	if cfg.ReviewStages[scale] == nil {
		return "auto", nil
	}
	mode := cfg.ReviewStages[scale][role]
	if mode == "" {
		return "auto", nil
	}
	return mode, nil
}
