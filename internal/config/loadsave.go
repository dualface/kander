package config

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/dualface/kander/v2/internal/fs"
)

func readConfigBytes(path string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		abs, err := lexicalAbsolute(path)
		if err != nil {
			return nil, err
		}
		anchor, err := volumeAnchor(abs)
		if err != nil {
			return nil, err
		}
		file, err := fs.OpenRegularFileIfExists(anchor, abs)
		if err != nil {
			return nil, err
		}
		if file == nil {
			return nil, nil
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, configErrorf("config.config_is_not_a_regular_file", path)
	}
	return os.ReadFile(path)
}

func loadValidated(path string, missingOK bool) (*Config, error) {
	cfg, _, err := loadScopeRawAt(path, missingOK)
	return cfg, err
}

func loadScopeRawAt(path string, missingOK bool) (*Config, map[string]any, error) {
	data, err := readConfigBytes(path)
	if err != nil {
		return nil, nil, configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	if data == nil {
		if !missingOK {
			return nil, nil, configErrorf("config.config_does_not_exist", path)
		}
		cfg := DefaultConfig()
		encoded, err := json.Marshal(cfg)
		if err != nil {
			return nil, nil, configErrorfWrap(err, "config.failed_to_read_config_2", err.Error())
		}
		raw, err := decodeJSON(encoded)
		if err != nil {
			return nil, nil, err
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, configErrorf("config.config_root_must_be_a_json_object")
		}
		return cfg, cloneRawObjectDeep(obj), nil
	}
	raw, err := decodeJSON(data)
	if err != nil {
		return nil, nil, configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	cfg, err := Validate(raw)
	if err != nil {
		return nil, nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, configErrorf("config.config_root_must_be_a_json_object")
	}
	return cfg, cloneRawObjectDeep(obj), nil
}

func loadEffective(cwd string, missingOK bool) (*Config, error) {
	layers, err := readLayers(cwd, missingOK)
	if err != nil {
		return nil, err
	}
	cfg, _, err := layers.effective()
	if err != nil {
		return nil, err
	}
	return layers.withPolicy(cfg), nil
}

// withPolicy applies the cached project policy over a configuration built from
// these layers. The policy is the last layer and covers only its five values;
// the result is a copy, so a cached scope configuration is never changed.
func (l layerDocuments) withPolicy(cfg *Config) *Config {
	out := Clone(cfg)
	scope := policyLayer{doc: l.scope}
	if exists, err := Exists(); err != nil || !exists {
		// A missing scope file reads as the defaults, which are no file values.
		scope.doc = nil
	}
	scope.path, _ = ConfigPath()
	applyPolicyCache(out, []policyLayer{scope, {path: l.overlayPath, doc: l.overlay}})
	out.layered = out.layered || l.overlay != nil
	return out
}

// layerDocuments are the raw documents of the two layers of one read: the scope file, already
// validated on its own, and the project overlay (nil when there is none). Load and the display
// of kander config both start from one value, so a run reads each file once.
type layerDocuments struct {
	scopeConfig *Config
	scope       map[string]any
	overlayPath string
	overlay     map[string]any
}

// readLayers reads the scope file and the project overlay of cwd (the process working
// directory when empty).
func readLayers(cwd string, missingOK bool) (layerDocuments, error) {
	path, err := ConfigPath()
	if err != nil {
		return layerDocuments{}, err
	}
	cfg, raw, err := loadScopeRawAt(path, missingOK)
	if err != nil {
		return layerDocuments{}, err
	}
	overlayPath, overlayRaw, err := readOverlay(cwd)
	if err != nil {
		return layerDocuments{}, err
	}
	return layerDocuments{scopeConfig: cfg, scope: raw, overlayPath: overlayPath, overlay: overlayRaw}, nil
}

// effective merges the layers and validates the result: the configuration in force, with the
// merged raw document it was validated from.
func (l layerDocuments) effective() (*Config, map[string]any, error) {
	return l.effectiveWith(LauncherNames)
}

// effectiveWith is effective with the launcher checked against the names that
// launchers returns.
func (l layerDocuments) effectiveWith(launchers func() []string) (*Config, map[string]any, error) {
	if l.overlay == nil {
		return l.scopeConfig, l.scope, nil
	}
	effective, merged, err := validateLayersWith(l.scope, l.overlay, launchers)
	if err != nil {
		return nil, nil, err
	}
	markOverlayIgnored(effective, l.scopeConfig, l.overlayPath, l.overlay)
	return effective, merged, nil
}

// validateLayers merges the raw scope document with the project overlay (none when empty) and
// validates the result, returning it with the merged document. It is the one place that turns
// layers into a Config: Load, the Options preview and the display of kander config all call it,
// so a configuration is merged, defaulted and checked for ignored content the same way wherever
// it comes from. The arguments are not modified.
func validateLayers(scope, overlay map[string]any) (*Config, map[string]any, error) {
	return validateLayersWith(scope, overlay, LauncherNames)
}

// validateLayersWith is validateLayers with the launcher checked against the
// names that launchers returns.
func validateLayersWith(scope, overlay map[string]any, launchers func() []string) (*Config, map[string]any, error) {
	if scope == nil {
		scope = map[string]any{}
	}
	base := cloneRawObjectDeep(scope)
	if len(overlay) == 0 {
		cfg, err := validateWithLaunchers(base, launchers)
		return cfg, base, err
	}
	merged, err := mergeOverlayRaw(base, overlay)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := validateWithLaunchers(merged, launchers)
	return cfg, merged, err
}

// markOverlayIgnored names the overlay as the source of the ignored items that
// the scope file does not carry, or that the overlay carries itself.
func markOverlayIgnored(effective, scope *Config, overlayPath string, overlay map[string]any) {
	inScope := make(map[string]struct{}, len(scope.Ignored))
	for _, item := range scope.Ignored {
		inScope[item.PathString()] = struct{}{}
	}
	for i := range effective.Ignored {
		item := &effective.Ignored[i]
		if _, ok := inScope[item.PathString()]; !ok || OverlayHas(overlay, item.Path...) {
			item.Source = overlayPath
		}
	}
}

// carryIgnored keeps what the source configuration ignored when dst was built
// again from a serialized copy of it, which cannot hold that content.
func carryIgnored(dst, src *Config) {
	if src == nil || dst == nil {
		return
	}
	known := make(map[string]struct{}, len(dst.Ignored))
	for _, item := range dst.Ignored {
		known[item.PathString()] = struct{}{}
	}
	for _, item := range src.Ignored {
		if _, ok := known[item.PathString()]; !ok {
			dst.Ignored = append(dst.Ignored, item)
		}
	}
	for name, reason := range src.UnusableAgents {
		if _, usable := dst.Agents[name]; usable {
			continue
		}
		if dst.UnusableAgents == nil {
			dst.UnusableAgents = map[string]string{}
		}
		dst.UnusableAgents[name] = reason
		if policy, ok := src.UnusablePolicy[name]; ok {
			if dst.UnusablePolicy == nil {
				dst.UnusablePolicy = map[string]AgentDefinition{}
			}
			dst.UnusablePolicy[name] = policy
		}
	}
}

// Load reads the scope config, merges a project overlay when present, and validates the result.
// Operational commands pass missingOK=false so a missing or invalid file fails instead of
// substituting DefaultConfig. A missing scope file yields the default config only when
// missingOK is true, which remains for Effective, doctor repair, and write-path helpers.
func Load(missingOK bool) (*Config, error) {
	return loadEffective("", missingOK)
}

// LoadFor is Load with the project overlay looked up from cwd instead of the
// process working directory, for checks that run against another checkout.
// A missing scope file fails, as for operational commands.
func LoadFor(cwd string) (*Config, error) {
	return loadEffective(cwd, false)
}

// LoadScope reads only the current-scope config.json and never applies a project overlay.
// Write and edit paths use this so overlay values cannot be written back into the scope file.
func LoadScope(missingOK bool) (*Config, error) {
	cfg, _, err := LoadScopeRaw(missingOK)
	return cfg, err
}

// LoadScopeRaw is LoadScope plus the decoded scope object, so callers can inspect
// explicit keys without reading the file a second time.
func LoadScopeRaw(missingOK bool) (*Config, map[string]any, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, nil, err
	}
	return loadScopeRawAt(path, missingOK)
}

// Exists reports whether config.json exists in the current scope, reusing the safety checks of config reads.
func Exists() (bool, error) {
	path, err := ConfigPath()
	if err != nil {
		return false, err
	}
	data, err := readConfigBytes(path)
	if err != nil {
		return false, configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	return data != nil, nil
}

// Effective returns the values in force at runtime; rule switches apply immediately, other choices only once initialization is complete.
func Effective(cfg *Config) (*Config, error) {
	if cfg == nil {
		loaded, err := Load(true)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	} else {
		encoded, err := json.Marshal(cfg)
		if err != nil {
			return nil, configErrorfWrap(err, "config.failed_to_read_config_2", err.Error())
		}
		raw, err := decodeJSON(encoded)
		if err != nil {
			return nil, err
		}
		validated, err := Validate(raw)
		if err != nil {
			return nil, err
		}
		carryIgnored(validated, cfg)
		validated.Policy, validated.policyLocal, validated.layered = cfg.Policy, cfg.policyLocal, cfg.layered
		cfg = validated
	}
	if cfg.WelcomeComplete {
		return cfg, nil
	}
	defaults := DefaultConfig()
	defaults.Rules = cfg.Rules.Clone()
	if cfg.Policy.Applied() {
		// The project policy applies whether or not this host finished its welcome.
		PolicyValuesOf(cfg).ApplyTo(defaults)
		defaults.Policy, defaults.policyLocal, defaults.layered = cfg.Policy, cfg.policyLocal, cfg.layered
	}
	// Service binding is independent of completing the interface welcome.
	defaults.Server, defaults.Project = cfg.Server, cfg.Project
	return defaults, nil
}

func encodeConfig(cfg *Config) ([]byte, error) {
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func wrapSaveError(path string, err error) *Error {
	if path == "" {
		return configErrorfWrap(err, "config.failed_to_write_config", err.Error())
	}
	return configErrorfWrap(err, "config.failed_to_write_config_2", path, err.Error())
}

func withConfigLock(path string, fn func() error) error {
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return err
	}
	return withConfigLockFile(abs, abs+".lock", fn)
}

func withConfigLockFile(dataPath, lockPath string, fn func() error) error {
	absData, err := lexicalAbsolute(dataPath)
	if err != nil {
		return err
	}
	absLock, err := lexicalAbsolute(lockPath)
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(absData), filepath.Dir(absLock)} {
		if err := fs.EnsureInheritedDirectoryPath(dir); err != nil {
			return wrapSaveError(dataPath, err)
		}
	}
	anchor, err := volumeAnchor(absLock)
	if err != nil {
		return err
	}
	lockFile, err := fs.OpenAppendFile(anchor, absLock)
	if err != nil {
		return wrapSaveError(dataPath, err)
	}
	lock, err := fs.LockExclusive(lockFile.File)
	if err != nil {
		_ = lockFile.Close()
		return wrapSaveError(dataPath, err)
	}
	operationErr := fn()
	unlockErr := lock.Unlock()
	closeErr := lockFile.Close()
	if operationErr != nil {
		return operationErr
	}
	if unlockErr != nil {
		return wrapSaveError(dataPath, unlockErr)
	}
	if closeErr != nil {
		return wrapSaveError(dataPath, closeErr)
	}
	return nil
}

// saveConfigAt validates and then atomically writes the given config.json.
//
// The file's current content is read inside the caller's lock, and what this version
// ignores in it (see Ignored) is written back into the new document unchanged.
func saveConfigAt(path string, cfg *Config) (string, error) {
	if cfg.layered {
		// Overlay and policy values sit in the same fields as the file's own; writing
		// them would move project settings into the scope file.
		return "", configErrorf("config.save_layered_config")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return "", wrapSaveError("", err)
	}
	raw, err := decodeJSON(encoded)
	if err != nil {
		return "", err
	}
	validated, err := Validate(raw)
	if err != nil {
		return "", err
	}
	// Validate rebuilt the config from its serialized copy, which cannot name an
	// agent this version took out of service; restore that, so the defaults it
	// filled in for such an agent do not shadow the values the file keeps.
	carryIgnored(validated, cfg)
	pruneUnusableModels(&validated.Models, validated)
	payload, err := encodeConfigKeeping(validated, ignoredOfFile(path))
	if err != nil {
		return "", wrapSaveError(path, err)
	}
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return "", err
	}
	anchor, err := volumeAnchor(abs)
	if err != nil {
		return "", err
	}
	if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(abs)); err != nil {
		return "", wrapSaveError(path, err)
	}
	if err := fs.WriteTextAtomicInherited(anchor, abs, string(payload), true); err != nil {
		return "", wrapSaveError(path, err)
	}
	return path, nil
}

// Save validates and then atomically writes config.json.
func Save(cfg *Config) (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	var saved string
	err = withConfigLock(path, func() error {
		var saveErr error
		saved, saveErr = saveConfigAt(path, cfg)
		return saveErr
	})
	return saved, err
}

// Update re-reads, mutates and saves the config inside a single cross-process lock.
func Update(mutate func(*Config) error) (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	return UpdateAt(path, mutate)
}

// UpdateAt is Update on an explicit config file, for an install that targets a
// scope other than the running binary's. A missing file starts from DefaultConfig.
func UpdateAt(path string, mutate func(*Config) error) (string, error) {
	var saved string
	err := withConfigLock(path, func() error {
		cfg, loadErr := loadValidated(path, true)
		if loadErr != nil {
			return loadErr
		}
		if mutate != nil {
			if mutateErr := mutate(cfg); mutateErr != nil {
				return mutateErr
			}
		}
		var saveErr error
		saved, saveErr = saveConfigAt(path, cfg)
		return saveErr
	})
	return saved, err
}

// SetLanguageIfPresent updates language in an existing config.json and does nothing when the file is missing.
func SetLanguageIfPresent(path, lang string) error {
	if !contains(Languages, lang) {
		return choiceError("language", strings.Join(Languages, ", "))
	}
	if path == "" {
		return nil
	}
	data, err := readConfigBytes(path)
	if err != nil {
		return configErrorfWrap(err, "config.failed_to_read_config", path, err.Error())
	}
	if data == nil {
		return nil
	}
	return withConfigLock(path, func() error {
		cfg, loadErr := loadValidated(path, false)
		if loadErr != nil {
			return loadErr
		}
		if cfg.Language == lang {
			return nil
		}
		cfg.Language = lang
		_, saveErr := saveConfigAt(path, cfg)
		return saveErr
	})
}

// nonZeroAgents drops the empty definitions of an Agents or UnusablePolicy map. In
// Agents, an entry that holds only content this version ignores reads back as an
// empty definition, and the same state before it was written is no entry at all. In
// UnusablePolicy, clearing the last policy field of an entry (the install switch,
// when no config_dir is set) leaves an empty entry in memory and none in the file.
// It returns a new map and never modifies its input.
func nonZeroAgents(m map[string]AgentDefinition) map[string]AgentDefinition {
	out := map[string]AgentDefinition{}
	for name, def := range m {
		if !isZeroAgentDefinition(def) {
			out[name] = def
		}
	}
	return out
}

func configEditConflicts(current, baseline, target *Config) bool {
	if current == nil || baseline == nil || target == nil {
		return true
	}
	currentValue := reflect.ValueOf(*current)
	baselineValue := reflect.ValueOf(*baseline)
	targetValue := reflect.ValueOf(*target)
	for i := 0; i < currentValue.NumField(); i++ {
		// What this version ignores is kept from the file at save time, so a change
		// to it by another writer is not a conflicting edit. The install switches of
		// an unusable entry are edited, so they take part in the check.
		if name := currentValue.Type().Field(i).Name; name == "Ignored" || name == "UnusableAgents" {
			continue
		}
		// The project policy state and the private load records describe a read, not a
		// value of the file.
		if field := currentValue.Type().Field(i); field.Name == "Policy" || !field.IsExported() {
			continue
		}
		currentField := currentValue.Field(i).Interface()
		baselineField := baselineValue.Field(i).Interface()
		targetField := targetValue.Field(i).Interface()
		switch currentValue.Type().Field(i).Name {
		case "UnusablePolicy":
			// An entry whose switch was cleared reads back as absent.
			currentField, baselineField, targetField = nonZeroAgents(current.UnusablePolicy), nonZeroAgents(baseline.UnusablePolicy), nonZeroAgents(target.UnusablePolicy)
		case "Agents":
			currentField, baselineField, targetField = nonZeroAgents(current.Agents), nonZeroAgents(baseline.Agents), nonZeroAgents(target.Agents)
		}
		if !reflect.DeepEqual(currentField, baselineField) && !reflect.DeepEqual(currentField, targetField) {
			return true
		}
	}
	return false
}

// SaveIfUnchanged saves only while the on-disk config still equals the editing baseline; a missing file may be created directly.
// The comparison reads the scope file alone, without the project overlay, so the
// baseline must come from a scope-only read (LoadScope or LoadScopeRaw). A
// baseline from Load carries the overlay's values and reports them as a
// concurrent edit.
func SaveIfUnchanged(cfg, baseline *Config) (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	var saved string
	err = withConfigLock(path, func() error {
		data, readErr := readConfigBytes(path)
		if readErr != nil {
			return configErrorfWrap(readErr, "config.failed_to_read_config", path, readErr.Error())
		}
		if data != nil {
			current, loadErr := loadValidated(path, false)
			if loadErr != nil {
				return loadErr
			}
			if configEditConflicts(current, baseline, cfg) {
				return configErrorf("config.configuration_changed_while_editing")
			}
		}
		var saveErr error
		saved, saveErr = saveConfigAt(path, cfg)
		return saveErr
	})
	return saved, err
}
