package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

const OverlayFilename = ".kander-config.json"

var overlayForbiddenKeys = map[string]struct{}{
	"schema_version":   {},
	"welcome_complete": {},
}

var overlayAllowedKeys = map[string]struct{}{
	"kanban_agent":   {},
	"kanban_agents":  {},
	"launcher":       {},
	"reviewers":      {},
	"review_stages":  {},
	"rules":          {},
	"models":         {},
	"tui":            {},
	"language":       {},
	"agent_language": {},
	"agents":         {},
}

func cloneRawValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneRawObjectDeep(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneRawValue(item)
		}
		return out
	default:
		return v
	}
}

func cloneRawObjectDeep(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = cloneRawValue(value)
	}
	return out
}

// deepMerge copies base and overlays src onto it: objects merge by key,
// scalars and arrays replace wholesale.
func deepMerge(base, overlay map[string]any) map[string]any {
	out := cloneRawObjectDeep(base)
	for key, value := range overlay {
		if overlayObj, ok := value.(map[string]any); ok {
			if baseObj, ok := out[key].(map[string]any); ok {
				out[key] = deepMerge(baseObj, overlayObj)
				continue
			}
		}
		out[key] = cloneRawValue(value)
	}
	return out
}

func normalizeReviewStagesField(raw map[string]any) error {
	stages, ok := raw["review_stages"]
	if !ok {
		return nil
	}
	normalized, err := NormalizeReviewStages(stages)
	if err != nil {
		return err
	}
	raw["review_stages"] = normalized
	return nil
}

func validateOverlayKeys(path string, obj map[string]any) error {
	var forbidden, unknown []string
	for key := range obj {
		if _, ok := overlayForbiddenKeys[key]; ok {
			forbidden = append(forbidden, key)
			continue
		}
		if _, ok := overlayAllowedKeys[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(forbidden) > 0 {
		return configErrorf("config.overlay_has_forbidden_keys", path, strings.Join(sorted(forbidden), ", "))
	}
	if len(unknown) > 0 {
		return configErrorf("config.overlay_has_unknown_keys", path, strings.Join(sorted(unknown), ", "))
	}
	return nil
}

func overlayPathSafe(path string) error {
	if runtime.GOOS == "windows" {
		_, err := ensureWindowsPathNofollowSafe(path)
		return err
	}
	return rejectLeafReparse(path)
}

func inspectOverlayCandidate(path string) (string, error) {
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return "", err
	}
	if err := overlayPathSafe(abs); err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", configErrorfWrap(err, "config.failed_to_inspect_path", abs)
	}
	if fs.IsReparsePoint(abs) {
		return "", unsafePathError(abs)
	}
	if !info.Mode().IsRegular() {
		return "", configErrorf("config.overlay_is_not_a_regular_file", abs)
	}
	return abs, nil
}

func walkOverlay(start string) (string, error) {
	current := start
	for {
		path, err := inspectOverlayCandidate(filepath.Join(current, OverlayFilename))
		if err != nil {
			return "", err
		}
		if path != "" {
			return path, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

// OverlayPath returns the absolute project overlay path for cwd, or "" when none exists.
// A Git directory uses the main worktree root; otherwise the search walks up from cwd.
func OverlayPath(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", configErrorfWrap(err, "config.failed_to_inspect_path", ".")
		}
	}
	start, err := lexicalAbsolute(cwd)
	if err != nil {
		return "", err
	}
	main, err := gitMainWorktree(start)
	if err != nil {
		return "", err
	}
	if main != "" {
		return inspectOverlayCandidate(filepath.Join(main, OverlayFilename))
	}
	return walkOverlay(start)
}

// ReadOverlay returns the absolute project overlay path and decoded object for cwd.
// The path is empty when no overlay file exists.
func ReadOverlay(cwd string) (string, map[string]any, error) {
	return readOverlay(cwd)
}

// ValidateOverlayMerge validates overlay against the unmerged scope object using
// the same raw merge as Load. It must not serialize a default-filled Config.
func ValidateOverlayMerge(scopeRaw map[string]any, overlay map[string]any) error {
	if overlay == nil {
		return nil
	}
	if scopeRaw == nil {
		return configErrorf("config.config_root_must_be_a_json_object")
	}
	merged, err := mergeOverlayRaw(cloneRawObjectDeep(scopeRaw), overlay)
	if err != nil {
		return err
	}
	_, err = Validate(merged)
	return err
}

// ApplyOverlay returns the validated config of scope with overlay merged on top.
// A nil overlay leaves the scope values unchanged.
func ApplyOverlay(scope *Config, overlay map[string]any) (*Config, error) {
	if scope == nil {
		return nil, configErrorf("config.config_root_must_be_a_json_object")
	}
	if overlay == nil {
		return Clone(scope), nil
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		return nil, configErrorfWrap(err, "config.failed_to_read_config_2", err.Error())
	}
	raw, err := decodeJSON(encoded)
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.config_root_must_be_a_json_object")
	}
	merged, err := mergeOverlayRaw(obj, overlay)
	if err != nil {
		return nil, err
	}
	return Validate(merged)
}

func readOverlay(cwd string) (string, map[string]any, error) {
	path, err := OverlayPath(cwd)
	if err != nil || path == "" {
		return path, nil, err
	}
	data, err := readConfigBytes(path)
	if err != nil {
		return "", nil, configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	if data == nil {
		return "", nil, nil
	}
	raw, err := decodeJSON(data)
	if err != nil {
		return "", nil, configErrorfWrap(err, "config.overlay_invalid_json", path, err.Error())
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return "", nil, configErrorf("config.overlay_root_must_be_a_json_object", path)
	}
	if err := validateOverlayKeys(path, obj); err != nil {
		return "", nil, err
	}
	return path, obj, nil
}

func mergeOverlayRaw(scope map[string]any, overlay map[string]any) (map[string]any, error) {
	if err := normalizeReviewStagesField(scope); err != nil {
		return nil, err
	}
	overlayCopy := cloneRawObjectDeep(overlay)
	if err := normalizeReviewStagesField(overlayCopy); err != nil {
		return nil, err
	}
	return deepMerge(scope, overlayCopy), nil
}
