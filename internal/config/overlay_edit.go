package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/dualface/kander/internal/fs"
)

const (
	// TargetScope is the Global tab: the install-scope or KANDER_CONFIG file.
	TargetScope = "scope"
	// TargetOverlay is the Project tab: always .kander-config.json.
	TargetOverlay = "overlay"
)

// OverlayLocation is the project overlay read/write target, including when the file does not exist yet.
type OverlayLocation struct {
	ProjectRoot string
	Path        string
	Exists      bool
}

// OptionsTargets returns the Options tabs for an install mode.
// A global binary always exposes both tabs; a project-install binary exposes only Project.
func OptionsTargets(mode Mode) []string {
	if mode == ModeProject {
		return []string{TargetOverlay}
	}
	return []string{TargetScope, TargetOverlay}
}

// CloneOverlay deep-copies a raw overlay object. A nil source becomes an empty map.
func CloneOverlay(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	return cloneRawObjectDeep(src)
}

// OverlayHas reports whether every path segment exists. A present false, empty
// array, or empty string counts as an explicit override, not as inherit.
func OverlayHas(overlay map[string]any, path ...string) bool {
	_, ok := OverlayGet(overlay, path...)
	return ok
}

// OverlayGet walks path and returns the value when the leaf key is present.
func OverlayGet(overlay map[string]any, path ...string) (any, bool) {
	if overlay == nil || len(path) == 0 {
		return nil, false
	}
	current := overlay
	for i, key := range path {
		value, ok := current[key]
		if !ok {
			return nil, false
		}
		if i == len(path)-1 {
			return value, true
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

// OverlaySet writes value at path, creating intermediate objects as needed.
func OverlaySet(overlay map[string]any, value any, path ...string) {
	if overlay == nil || len(path) == 0 {
		return
	}
	current := overlay
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = overlayJSONValue(value)
}

// OverlayDelete removes the leaf key and prunes empty parent objects so a
// leftover {"tui":{}} cannot fail merged TUI validation.
func OverlayDelete(overlay map[string]any, path ...string) {
	if overlay == nil || len(path) == 0 {
		return
	}
	overlayDelete(overlay, path)
}

func overlayDelete(current map[string]any, path []string) bool {
	key := path[0]
	if len(path) == 1 {
		delete(current, key)
		return len(current) == 0
	}
	child, ok := current[key].(map[string]any)
	if !ok {
		return false
	}
	if overlayDelete(child, path[1:]) && len(child) == 0 {
		delete(current, key)
	}
	return len(current) == 0
}

func overlayJSONValue(value any) any {
	switch typed := value.(type) {
	case int:
		return json.Number(strconv.Itoa(typed))
	case bool, string:
		return typed
	default:
		return cloneRawValue(typed)
	}
}

// ResolveOverlayLocation returns the overlay path that Options will read and write.
// Git uses the main worktree; a missing file still names that path. Non-Git reuses
// walk-up when a file exists, otherwise the start directory is the create target.
func ResolveOverlayLocation(cwd string) (OverlayLocation, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return OverlayLocation{}, configErrorfWrap(err, "config.failed_to_inspect_path", ".")
		}
	}
	start, err := lexicalAbsolute(cwd)
	if err != nil {
		return OverlayLocation{}, err
	}
	main, err := gitMainWorktree(start)
	if err != nil {
		return OverlayLocation{}, err
	}
	if main != "" {
		return overlayLocationAt(main)
	}
	existing, err := walkOverlay(start)
	if err != nil {
		return OverlayLocation{}, err
	}
	if existing != "" {
		return OverlayLocation{
			ProjectRoot: filepath.Dir(existing),
			Path:        existing,
			Exists:      true,
		}, nil
	}
	return overlayLocationAt(start)
}

func overlayLocationAt(root string) (OverlayLocation, error) {
	path := filepath.Join(root, OverlayFilename)
	found, err := inspectOverlayCandidate(path)
	if err != nil {
		return OverlayLocation{}, err
	}
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return OverlayLocation{}, err
	}
	return OverlayLocation{
		ProjectRoot: root,
		Path:        abs,
		Exists:      found != "",
	}, nil
}

// ReadOverlayFile loads a known overlay path. A missing file yields an empty map.
func ReadOverlayFile(path string) (map[string]any, error) {
	if path == "" {
		return map[string]any{}, nil
	}
	if err := overlayPathSafe(path); err != nil {
		return nil, err
	}
	data, err := readConfigBytes(path)
	if err != nil {
		return nil, configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	if data == nil {
		return map[string]any{}, nil
	}
	raw, err := decodeJSON(data)
	if err != nil {
		return nil, configErrorfWrap(err, "config.overlay_invalid_json", path, err.Error())
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.overlay_root_must_be_a_json_object", path)
	}
	if err := validateOverlayKeys(path, obj); err != nil {
		return nil, err
	}
	return cloneRawObjectDeep(obj), nil
}

// DocumentFromConfig encodes a filled Config as a JSON object. In-memory
// sessions use this when no original document exists. Overlay save and Load
// must use LoadScopeDocument so missing optional sections stay missing.
func DocumentFromConfig(cfg *Config) (map[string]any, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	encoded, err := json.Marshal(cfg)
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
	return obj, nil
}

// LoadScopeDocument returns the raw scope JSON object used by overlay merge.
func LoadScopeDocument(missingOK bool) (map[string]any, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	_, raw, err := loadScopeRawAt(path, missingOK)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// MergeOverlayOnRaw validates overlay keys against a raw scope JSON document,
// matching Load. Do not pass a filled Config: missing optional sections would
// be filled and then accept overlays that Load rejects.
func MergeOverlayOnRaw(scopeRaw, overlay map[string]any) (*Config, error) {
	if scopeRaw == nil {
		scopeRaw = map[string]any{}
	}
	base := cloneRawObjectDeep(scopeRaw)
	if len(overlay) == 0 {
		return Validate(base)
	}
	merged, err := mergeOverlayRaw(base, overlay)
	if err != nil {
		return nil, err
	}
	return Validate(merged)
}

// MergeScopeAndOverlay validates a filled Config plus sparse overlay keys.
// Overlay save and Project-tab preview use MergeOverlayOnRaw instead.
func MergeScopeAndOverlay(scope *Config, overlay map[string]any) (*Config, error) {
	raw, err := DocumentFromConfig(scope)
	if err != nil {
		return nil, err
	}
	return MergeOverlayOnRaw(raw, overlay)
}

func encodeOverlay(obj map[string]any) ([]byte, error) {
	if obj == nil {
		obj = map[string]any{}
	}
	payload, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func overlayMapsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	left, err := encodeOverlay(a)
	if err != nil {
		return false
	}
	right, err := encodeOverlay(b)
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

func readOverlayMapAt(path string) (map[string]any, bool, error) {
	data, err := readConfigBytes(path)
	if err != nil {
		return nil, false, configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	if data == nil {
		return map[string]any{}, false, nil
	}
	raw, err := decodeJSON(data)
	if err != nil {
		return nil, true, configErrorfWrap(err, "config.overlay_invalid_json", path, err.Error())
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, true, configErrorf("config.overlay_root_must_be_a_json_object", path)
	}
	if err := validateOverlayKeys(path, obj); err != nil {
		return nil, true, err
	}
	return cloneRawObjectDeep(obj), true, nil
}

func removeOverlayFile(path string) error {
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return err
	}
	anchor, err := volumeAnchor(abs)
	if err != nil {
		return err
	}
	if _, err := fs.RemoveRegularFileIfExists(anchor, abs); err != nil {
		return wrapSaveError(path, err)
	}
	return nil
}

func writeOverlayAt(path string, overlay map[string]any) error {
	if err := validateOverlayKeys(path, overlay); err != nil {
		return err
	}
	payload, err := encodeOverlay(overlay)
	if err != nil {
		return wrapSaveError(path, err)
	}
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return err
	}
	anchor, err := volumeAnchor(abs)
	if err != nil {
		return err
	}
	if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(abs)); err != nil {
		return wrapSaveError(path, err)
	}
	if err := fs.WriteTextAtomicInherited(anchor, abs, string(payload), true); err != nil {
		return wrapSaveError(path, err)
	}
	return nil
}

// ErrMissingOverlayPath is returned when Options has no resolved write target.
func ErrMissingOverlayPath() error {
	return configErrorf("config.failed_to_inspect_path", ".")
}

// SaveOverlayIfUnchanged writes only explicit overlay keys after validating the
// merged effective config. An empty overlay deletes the file and never creates one.
func SaveOverlayIfUnchanged(path string, overlay, baseline map[string]any) (string, error) {
	if path == "" {
		return "", configErrorf("config.failed_to_inspect_path", ".")
	}
	if overlay == nil {
		overlay = map[string]any{}
	}
	if err := validateOverlayKeys(path, overlay); err != nil {
		return "", err
	}
	raw, err := LoadScopeDocument(true)
	if err != nil {
		return "", err
	}
	if _, err := MergeOverlayOnRaw(raw, overlay); err != nil {
		return "", err
	}
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return "", err
	}
	lockPath, err := overlayAdvisoryLock(abs)
	if err != nil {
		return "", err
	}
	err = withConfigLockFile(abs, lockPath, func() error {
		current, _, readErr := readOverlayMapAt(abs)
		if readErr != nil {
			return readErr
		}
		if !overlayMapsEqual(current, baseline) {
			return configErrorf("config.configuration_changed_while_editing")
		}
		if len(overlay) == 0 {
			return removeOverlayFile(abs)
		}
		return writeOverlayAt(abs, overlay)
	})
	if err != nil {
		return "", err
	}
	return abs, nil
}

// overlayAdvisoryLock keeps the exclusive lock out of the project tree so a
// save cannot leave .kander-config.json.lock next to user sources.
func overlayAdvisoryLock(overlay string) (string, error) {
	cfgPath, err := ConfigPath()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(overlay))
	return filepath.Join(filepath.Dir(cfgPath), "overlay-"+hex.EncodeToString(sum[:8])+".lock"), nil
}
