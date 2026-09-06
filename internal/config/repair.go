package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// RepairResult records what doctor changed in the on-disk config, without holding any config value.
type RepairResult struct {
	Path       string
	BackupPath string
	Created    bool
	Changed    bool
}

// Repair fixes the schema, then calls adjust for environment-dependent choices, and backs up and atomically saves once validation passes.
// A plain Load never repairs; a read failure leaves the file untouched, and a config that is already valid and needs no adjustment is not rewritten.
func Repair(adjust func(*Config)) (*Config, RepairResult, error) {
	path, err := ConfigPath()
	result := RepairResult{Path: path}
	if err != nil {
		return nil, result, err
	}
	var cfg *Config
	err = withConfigLock(path, func() error {
		var repairErr error
		cfg, result, repairErr = repairAt(path, adjust)
		return repairErr
	})
	return cfg, result, err
}

func repairAt(path string, adjust func(*Config)) (*Config, RepairResult, error) {
	result := RepairResult{Path: path}
	abs, err := lexicalAbsolute(path)
	if err != nil {
		return nil, result, err
	}
	anchor, err := volumeAnchor(abs)
	if err != nil {
		return nil, result, err
	}
	data, exists, err := fs.ReadRegularFileIfExists(anchor, abs)
	if err != nil {
		return nil, result, err
	}
	raw, _ := decodeJSON(data)
	// Broken JSON is never guessed at; a parseable object keeps every legal setting field by field.
	if !json.Valid(data) {
		raw = nil
	}
	cfg, err := repairValues(raw)
	if err != nil {
		return nil, result, err
	}
	if adjust != nil {
		adjust(cfg)
	}
	payload, err := encodeConfig(cfg)
	if err != nil {
		return nil, result, err
	}
	if _, err := ValidateJSON(payload); err != nil {
		return nil, result, err
	}
	normalized, err := decodeJSON(payload)
	if err != nil {
		return nil, result, err
	}
	if exists && reflect.DeepEqual(raw, normalized) {
		return cfg, result, nil
	}
	if exists {
		backup := abs + ".bak." + time.Now().UTC().Format("20060102T150405.000000000")
		if err := fs.WriteTextAtomicInherited(anchor, backup, string(data), false); err != nil {
			return nil, result, err
		}
		result.BackupPath = backup
	}
	// Directory creation and permission behavior follow Save; no legacy config migration or private permission check is introduced.
	if _, err := saveConfigAt(filepath.Clean(path), cfg); err != nil {
		return nil, result, err
	}
	result.Created = !exists
	result.Changed = true
	return cfg, result, nil
}

func repairValues(raw any) (*Config, error) {
	defaults := DefaultConfig()
	// doctor defaults to English when the config names no valid language.
	defaults.Language = "en"
	if lang := CLILanguage(); contains(Languages, lang) {
		defaults.Language = lang
	}
	provided, _ := asObject(raw)
	// A new config defaults to all rules on. An existing but broken rules section is restored field by field from an all-off baseline, so
	// the on-by-default task_groups cannot stop doctor from preserving a git switch the user explicitly turned off.
	if _, exists := provided["rules"]; exists {
		defaults.Rules = DefaultRules(false)
	} else {
		// Preserve legacy rule activation without replacing doctor's other field defaults.
		if _, err := Validate(raw); err == nil {
			defaults.Rules = legacyRules()
		}
	}
	if agent, err := validateChoice(provided["kanban_agent"], ExecutionAgents, "kanban_agent"); err == nil {
		defaults.KanbanAgent = agent
		for _, scale := range TaskScales {
			defaults.KanbanAgents[scale] = agent
		}
	}
	data, err := encodeConfig(defaults)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeJSON(data)
	if err != nil {
		return nil, err
	}
	root, _ := asObject(decoded)
	recoverConfigFields(root, provided, root)
	return Validate(root)
}

// A wholly valid section is accepted as is; otherwise it is restored key by key from the default schema, and validation always goes through Validate.
func recoverConfigFields(target, provided, root map[string]any) {
	keys := make([]string, 0, len(target))
	for key := range target {
		keys = append(keys, key)
	}
	for _, key := range sorted(keys) {
		value, exists := provided[key]
		if !exists {
			continue
		}
		previous := target[key]
		target[key] = value
		if _, err := Validate(root); err == nil {
			continue
		}
		target[key] = previous
		oldObject, oldOK := asObject(previous)
		newObject, newOK := asObject(value)
		if oldOK && newOK {
			recoverConfigFields(oldObject, newObject, root)
		}
	}
}
