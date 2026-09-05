package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// RepairResult 记录 doctor 对磁盘配置的变更, 不包含配置值.
type RepairResult struct {
	Path       string
	BackupPath string
	Created    bool
	Changed    bool
}

// Repair 修复 schema 后调用 adjust 调整环境相关选择, 校验通过后备份并原子保存.
// 普通 Load 不修复; 读取失败时不覆盖文件, 已有效且无需调整的配置不重写.
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
	// 损坏 JSON 不尝试猜测内容; 可解析的 object 逐字段保留合法设置.
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
	// 创建目录与权限行为沿用 Save, 不引入旧配置迁移或私有权限检查.
	if _, err := saveConfigAt(filepath.Clean(path), cfg); err != nil {
		return nil, result, err
	}
	result.Created = !exists
	result.Changed = true
	return cfg, result, nil
}

func repairValues(raw any) (*Config, error) {
	defaults := DefaultConfig()
	// doctor 在配置未指定有效语言时默认使用英文.
	defaults.Language = "en"
	provided, _ := asObject(raw)
	// 新配置默认全开。已有但损坏的 rules 从全关基线逐项恢复，避免默认启用的
	// task_groups 阻止 doctor 保留用户明确关闭的 git。
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

// 整段合法时直接接受; 否则按默认 schema 的键向下恢复, 校验始终复用 Validate.
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
