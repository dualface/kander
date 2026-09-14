package config

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/dualface/kander/internal/fs"
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

func loadEffective(missingOK bool) (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, raw, err := loadScopeRawAt(path, missingOK)
	if err != nil {
		return nil, err
	}
	_, overlayRaw, err := readOverlay("")
	if err != nil {
		return nil, err
	}
	if overlayRaw == nil {
		return cfg, nil
	}
	merged, err := mergeOverlayRaw(raw, overlayRaw)
	if err != nil {
		return nil, err
	}
	return Validate(merged)
}

// Load reads the scope config, merges a project overlay when present, and validates the result.
// Operational commands pass missingOK=false so a missing or invalid file fails instead of
// substituting DefaultConfig. A missing scope file yields the default config only when
// missingOK is true, which remains for Effective, doctor repair, and write-path helpers.
func Load(missingOK bool) (*Config, error) {
	return loadEffective(missingOK)
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
		cfg = validated
	}
	if cfg.WelcomeComplete {
		return cfg, nil
	}
	defaults := DefaultConfig()
	defaults.Rules = cfg.Rules.Clone()
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
func saveConfigAt(path string, cfg *Config) (string, error) {
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
	payload, err := encodeConfig(validated)
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
	var saved string
	err = withConfigLock(path, func() error {
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

func configEditConflicts(current, baseline, target *Config) bool {
	if current == nil || baseline == nil || target == nil {
		return true
	}
	currentValue := reflect.ValueOf(*current)
	baselineValue := reflect.ValueOf(*baseline)
	targetValue := reflect.ValueOf(*target)
	for i := 0; i < currentValue.NumField(); i++ {
		currentField := currentValue.Field(i).Interface()
		baselineField := baselineValue.Field(i).Interface()
		targetField := targetValue.Field(i).Interface()
		if !reflect.DeepEqual(currentField, baselineField) && !reflect.DeepEqual(currentField, targetField) {
			return true
		}
	}
	return false
}

// SaveIfUnchanged saves only while the on-disk config still equals the editing baseline; a missing file may be created directly.
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
