package config

// mergeReviewModelOwners keeps a role's inherited values with their source
// reviewer. A project binding to another reviewer starts from that agent's
// defaults; deleting one project field must not expose another agent's value.
// Inputs have normalized reviewer shapes but still need ordinary validation.
func mergeReviewModelOwners(scope, overlay, merged map[string]any) {
	for _, role := range ReviewRoles {
		entry, _ := OverlayGet(merged, "models", "review_roles", role)
		if _, ok := entry.(map[string]any); !ok {
			continue
		}
		for _, scale := range TaskScales {
			path := []string{"models", "review_roles", role}
			ownerKey := scale + "_agent"
			baseOwner := rawReviewModelString(scope, role, ownerKey)
			if baseOwner == "" {
				baseOwner = rawReviewer(scope, scale, role)
			}
			owner := rawReviewModelString(overlay, role, ownerKey)
			if owner == "" {
				owner = baseOwner
				for _, field := range []string{"model", "effort", scale + "_model", scale + "_effort"} {
					if OverlayHas(overlay, append(path, field)...) {
						owner = rawReviewer(merged, scale, role)
						break
					}
				}
			}
			if owner == baseOwner && owner == rawReviewer(merged, scale, role) {
				// Preserve compatible legacy fallback in either direction without
				// replacing a nonempty scale value or an explicit Project scale key.
				var legacy map[string]any
				projectBound := rawReviewModelString(overlay, role, ownerKey) != ""
				scopeBound := rawReviewModelString(scope, role, ownerKey) != ""
				if projectBound && !scopeBound {
					legacy = merged
				} else if !projectBound && scopeBound {
					legacy = overlay
				}
				for _, field := range []string{"model", "effort"} {
					key := scale + "_" + field
					if !OverlayHas(overlay, append(path, key)...) && rawReviewModelString(merged, role, key) == "" {
						if value, ok := OverlayGet(legacy, append(path, field)...); ok {
							OverlaySet(merged, value, append(path, key)...)
						}
					}
				}
				continue
			}
			// Do not replace malformed explicit values: validation must reject them.
			if !OverlayHas(overlay, append(path, ownerKey)...) {
				OverlaySet(merged, owner, append(path, ownerKey)...)
			}
			if owner == baseOwner {
				continue
			}
			for _, field := range []string{"model", "effort"} {
				key := scale + "_" + field
				if !OverlayHas(overlay, append(path, key)...) {
					value := any("")
					if shared, ok := OverlayGet(overlay, append(path, field)...); ok {
						value = shared
					}
					OverlaySet(merged, value, append(path, key)...)
				}
			}
		}
	}
}

func rawReviewModelString(raw map[string]any, role, field string) string {
	value, _ := OverlayGet(raw, "models", "review_roles", role, field)
	text, _ := value.(string)
	return text
}

func rawReviewer(raw map[string]any, scale, role string) string {
	value, _ := OverlayGet(raw, "reviewers", scale, role)
	if agent, ok := value.(string); ok && agent != "" {
		return agent
	}
	return defaultAgentName()
}
