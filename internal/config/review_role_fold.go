package config

import "strings"

// Historical four-role names remain readable in old documents. They fold into
// the two current roles before validation materializes a Config.
var historicalReviewRoles = []string{"PM", "QA", "CSA", "Hacker", "PMQA", "Security"}

var reviewRoleSources = map[string][]string{
	"PMQA":     {"PMQA", "PM", "QA"},
	"Security": {"Security", "CSA", "Hacker"},
}

var reviewRoleModelFields = []string{
	"model", "effort",
	"large_model", "small_model",
	"large_effort", "small_effort",
	"large_agent", "small_agent",
}

func historicalReviewRoleSet() map[string]struct{} {
	allowed := make(map[string]struct{}, len(historicalReviewRoles))
	for _, role := range historicalReviewRoles {
		allowed[role] = struct{}{}
	}
	return allowed
}

func currentReviewRoleSet() map[string]struct{} {
	allowed := make(map[string]struct{}, len(ReviewRoles))
	for _, role := range ReviewRoles {
		allowed[role] = struct{}{}
	}
	return allowed
}

// ReviewRoleSourceNames is the fold order for one current role: itself, then
// the first constituent, then the second. Unknown names return only themselves.
func ReviewRoleSourceNames(role string) []string {
	if sources, ok := reviewRoleSources[role]; ok {
		return append([]string{}, sources...)
	}
	return []string{role}
}

func stageRank(mode string) int {
	switch mode {
	case "skip":
		return 0
	case "auto":
		return 1
	case "required":
		return 2
	default:
		return -1
	}
}

func foldReviewStageModes(modes map[string]string) map[string]string {
	out := make(map[string]string, len(ReviewRoles))
	for _, target := range ReviewRoles {
		strongest := ""
		rank := -1
		for _, src := range reviewRoleSources[target] {
			mode, ok := modes[src]
			if !ok {
				continue
			}
			if r := stageRank(mode); r > rank {
				rank = r
				strongest = mode
			}
		}
		if rank >= 0 {
			out[target] = strongest
		}
	}
	return out
}

func foldReviewerAgents(values map[string]string) map[string]string {
	def := defaultAgentName()
	out := make(map[string]string, len(ReviewRoles))
	for _, target := range ReviewRoles {
		chosen := ""
		saw := false
		for _, src := range reviewRoleSources[target] {
			agent, ok := values[src]
			if !ok {
				continue
			}
			saw = true
			if agent != "" && agent != def {
				chosen = agent
				break
			}
			if chosen == "" && agent != "" {
				chosen = agent
			}
		}
		if !saw {
			continue
		}
		if chosen == "" {
			chosen = def
		}
		out[target] = chosen
	}
	return out
}

func foldReviewRoleModelEntries(roles map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(ReviewRoles))
	for _, target := range ReviewRoles {
		entry := map[string]string{}
		anyField := false
		for _, field := range reviewRoleModelFields {
			for _, src := range reviewRoleSources[target] {
				if srcEntry, ok := roles[src]; ok {
					if value := srcEntry[field]; value != "" {
						entry[field] = value
						anyField = true
						break
					}
				}
			}
		}
		if anyField {
			out[target] = entry
		}
	}
	return out
}

func historicalKeysPresent(obj map[string]any) bool {
	allowed := historicalReviewRoleSet()
	for key := range obj {
		if _, ok := allowed[key]; ok {
			return true
		}
	}
	return false
}

func foldRawStageRoleMap(obj map[string]any) map[string]any {
	if obj == nil {
		return obj
	}
	modes := map[string]string{}
	allowed := historicalReviewRoleSet()
	for key, value := range obj {
		if _, ok := allowed[key]; !ok {
			continue
		}
		text, ok := value.(string)
		if !ok || stageRank(text) < 0 {
			return cloneRawObject(obj)
		}
		modes[key] = text
	}
	if len(modes) == 0 {
		return cloneRawObject(obj)
	}
	folded := foldReviewStageModes(modes)
	out := map[string]any{}
	for key, value := range obj {
		if _, ok := allowed[key]; ok {
			continue
		}
		out[key] = cloneRawValue(value)
	}
	for role, mode := range folded {
		out[role] = mode
	}
	return out
}

func foldRawReviewerRoleMap(obj map[string]any) map[string]any {
	if obj == nil {
		return obj
	}
	values := map[string]string{}
	allowed := historicalReviewRoleSet()
	for key, value := range obj {
		if _, ok := allowed[key]; !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return cloneRawObject(obj)
		}
		values[key] = text
	}
	if len(values) == 0 {
		return cloneRawObject(obj)
	}
	folded := foldReviewerAgents(values)
	out := map[string]any{}
	for key, value := range obj {
		if _, ok := allowed[key]; ok {
			continue
		}
		out[key] = cloneRawValue(value)
	}
	for role, agent := range folded {
		out[role] = agent
	}
	return out
}

func foldRawReviewSection(doc map[string]any, key string, foldMap func(map[string]any) map[string]any) {
	raw, exists := doc[key]
	if !exists {
		return
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return
	}
	scales, roles, unknown := classifyReviewStages(obj)
	if len(scales) > 0 && len(roles) > 0 {
		return
	}
	if len(scales) > 0 && len(unknown) == 0 {
		out := cloneRawObject(obj)
		for _, scale := range scales {
			nested, ok := out[scale].(map[string]any)
			if !ok {
				continue
			}
			out[scale] = foldMap(nested)
		}
		doc[key] = out
		return
	}
	if (len(roles) > 0 || historicalKeysPresent(obj)) && len(scales) == 0 {
		doc[key] = foldMap(obj)
	}
}

func foldRawReviewRoleModels(doc map[string]any) {
	modelsRaw, ok := doc["models"].(map[string]any)
	if !ok {
		return
	}
	rolesRaw, ok := modelsRaw["review_roles"].(map[string]any)
	if !ok {
		return
	}
	allowed := historicalReviewRoleSet()
	parsed := map[string]map[string]string{}
	for role, entryRaw := range rolesRaw {
		if _, ok := allowed[role]; !ok {
			return
		}
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return
		}
		fields := map[string]string{}
		for field, value := range entry {
			text, ok := value.(string)
			if !ok {
				return
			}
			fields[field] = text
		}
		parsed[role] = fields
	}
	folded := foldReviewRoleModelEntries(parsed)
	out := map[string]any{}
	for role, entry := range folded {
		fields := map[string]any{}
		for field, value := range entry {
			fields[field] = value
		}
		out[role] = fields
	}
	modelsRaw["review_roles"] = out
}

// FoldLegacyReviewRoleKeys rewrites review_stages, reviewers, and
// models.review_roles in a raw document so only current roles remain.
// Mixed scale/role objects and invalid stage values are left for Validate.
func FoldLegacyReviewRoleKeys(doc map[string]any) {
	if doc == nil {
		return
	}
	foldRawReviewSection(doc, "review_stages", foldRawStageRoleMap)
	foldRawReviewSection(doc, "reviewers", foldRawReviewerRoleMap)
	foldRawReviewRoleModels(doc)
}

func unknownHistoricalRoles(obj map[string]any) []string {
	allowed := historicalReviewRoleSet()
	var unknown []string
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	return sorted(unknown)
}

func reviewRoleFieldSchema() map[string]string {
	return map[string]string{
		"model": "", "effort": "",
		"large_model": "", "small_model": "",
		"large_effort": "", "small_effort": "",
		"large_agent": "", "small_agent": "",
	}
}

func validateAndFoldReviewRoleModels(provided map[string]any) (map[string]map[string]string, error) {
	unknown := unknownHistoricalRoles(provided)
	if len(unknown) > 0 {
		return nil, configErrorf(
			"config.models_has_unknown_agents", "review_roles", strings.Join(unknown, ", "),
		)
	}
	schema := reviewRoleFieldSchema()
	parsed := make(map[string]map[string]string, len(provided))
	for role, entryRaw := range provided {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return nil, configErrorf("config.models_must_be_a_json_object_3", "review_roles", role)
		}
		var unknownFields []string
		for field := range entry {
			if _, ok := schema[field]; !ok {
				unknownFields = append(unknownFields, field)
			}
		}
		if len(unknownFields) > 0 {
			return nil, configErrorf(
				"config.models_has_unknown_fields", "review_roles", role, strings.Join(sorted(unknownFields), ", "),
			)
		}
		fields := map[string]string{}
		for field, value := range entry {
			text, ok := value.(string)
			if !ok {
				return nil, configErrorf(
					"config.models_must_be_a", "review_roles", role, field, Text("config.string"),
				)
			}
			if strings.ContainsAny(text, "\n\r\x00") {
				return nil, configErrorf(
					"config.models_must_not_contain_line_breaks_or_nul", "review_roles", role, field,
				)
			}
			if strings.HasSuffix(field, "_agent") && text != "" && !ValidAgentName(text) {
				return nil, agentDefinitionError("models.review_roles."+role+"."+field, Text("config.agent_name"))
			}
			fields[field] = text
		}
		parsed[role] = fields
	}
	return foldReviewRoleModelEntries(parsed), nil
}

// OverlayHasReviewPath reports overlay presence for a current review role,
// including historical constituent keys on the original document.
func OverlayHasReviewPath(overlay map[string]any, path ...string) bool {
	if OverlayHas(overlay, path...) {
		return true
	}
	if len(path) == 3 && (path[0] == "review_stages" || path[0] == "reviewers") {
		return overlayHasReviewRole(overlay, path[0], path[1], path[2])
	}
	if len(path) == 4 && path[0] == "models" && path[1] == "review_roles" {
		return overlayHasReviewRoleModel(overlay, path[2], path[3])
	}
	if len(path) == 3 && path[0] == "models" && path[1] == "review_roles" {
		return overlayHasReviewRoleModel(overlay, path[2], "")
	}
	return false
}

func overlayHasReviewRole(overlay map[string]any, section, scale, role string) bool {
	for _, src := range ReviewRoleSourceNames(role) {
		if OverlayHas(overlay, section, scale, src) || OverlayHas(overlay, section, src) {
			return true
		}
	}
	return false
}

func overlayHasReviewRoleModel(overlay map[string]any, role, field string) bool {
	for _, src := range ReviewRoleSourceNames(role) {
		if field == "" {
			if OverlayHas(overlay, "models", "review_roles", src) {
				return true
			}
			continue
		}
		if OverlayHas(overlay, "models", "review_roles", src, field) {
			return true
		}
	}
	return false
}

// OverlayDeleteReviewRole removes a current role and its historical sources
// from a section, both the scaled path and a flat role key.
func OverlayDeleteReviewRole(overlay map[string]any, section, scale, role string) {
	for _, src := range ReviewRoleSourceNames(role) {
		OverlayDelete(overlay, section, scale, src)
		OverlayDelete(overlay, section, src)
	}
}

// OverlayDeleteReviewRoleModel removes a current role's model fields and the
// same fields on historical sources.
func OverlayDeleteReviewRoleModel(overlay map[string]any, role string, field string) {
	for _, src := range ReviewRoleSourceNames(role) {
		if field == "" {
			OverlayDelete(overlay, "models", "review_roles", src)
			continue
		}
		OverlayDelete(overlay, "models", "review_roles", src, field)
	}
}
