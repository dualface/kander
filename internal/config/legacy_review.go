package config

import "sort"

// LegacyReviewKeys returns sorted paths mapped or ignored during this load.
// PM is ignored; CSA/Hacker map to Security. The returned slice is independent.
// Diagnostics are not serialized or persisted in configuration documents.
func (c *Config) LegacyReviewKeys() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.legacyReviewKeys...)
}

// NormalizeLegacyReviewKeys returns a copy with legacy role keys converted.
// Explicit Security keys win within a layer. Normalize each layer before merging
// so a legacy project override still takes precedence over the scope config.
func NormalizeLegacyReviewKeys(raw map[string]any) (map[string]any, []string) {
	out := cloneRawObjectDeep(raw)
	var keys []string
	convert := func(roles map[string]any, path string, stages bool) {
		for _, role := range []string{"PM", "CSA", "Hacker"} {
			if _, exists := roles[role]; exists {
				keys = append(keys, path+"."+role)
			}
		}
		if _, exists := roles["Security"]; !exists {
			csa, hasCSA := roles["CSA"]
			hacker, hasHacker := roles["Hacker"]
			if hasCSA || hasHacker {
				value := csa
				if !hasCSA {
					value = hacker
				}
				if stages {
					// Missing policies retain their existing auto default.
					if !hasCSA {
						csa = "auto"
					}
					if !hasHacker {
						hacker = "auto"
					}
					switch {
					case !legacyReviewStageValid(csa):
						value = csa
					case !legacyReviewStageValid(hacker):
						value = hacker
					case csa == "required" || hacker == "required":
						value = "required"
					case csa == "auto" || hacker == "auto":
						value = "auto"
					default:
						value = "skip"
					}
				}
				roles["Security"] = value
			}
		}
		delete(roles, "PM")
		delete(roles, "CSA")
		delete(roles, "Hacker")
	}
	for _, field := range []string{"reviewers", "review_stages"} {
		obj, ok := out[field].(map[string]any)
		if !ok {
			continue
		}
		convert(obj, field, field == "review_stages")
		for _, scale := range TaskScales {
			if roles, ok := obj[scale].(map[string]any); ok {
				convert(roles, field+"."+scale, field == "review_stages")
			}
		}
	}
	if models, ok := out["models"].(map[string]any); ok {
		if roles, ok := models["review_roles"].(map[string]any); ok {
			convert(roles, "models.review_roles", false)
		}
	}
	sort.Strings(keys)
	return out, keys
}

func validateMergedReviewKeys(merged map[string]any, layers ...map[string]any) (*Config, error) {
	cfg, err := Validate(merged)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, layer := range layers {
		_, keys := NormalizeLegacyReviewKeys(layer)
		for _, key := range keys {
			seen[key] = true
		}
	}
	for key := range seen {
		cfg.legacyReviewKeys = append(cfg.legacyReviewKeys, key)
	}
	sort.Strings(cfg.legacyReviewKeys)
	return cfg, nil
}

// Preserve malformed legacy values so ordinary schema validation rejects them,
// instead of concealing an invalid policy behind its sibling's higher priority.
func legacyReviewStageValid(value any) bool {
	mode, ok := value.(string)
	return ok && contains(ReviewStageModes, mode)
}
