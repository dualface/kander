package config

import (
	"os"
	"strings"
	"sync"

	"github.com/dualface/kander/internal/i18n"
)

var (
	langMu              sync.Mutex
	cliLanguageOverride string
	configLanguage      string
)

// ApplyLanguageArgument 扫描 argv 中的 --lang, 并设置 KANDER_LANG / KANDER_LANG_CLI.
func ApplyLanguageArgument(arguments []string) {
	langMu.Lock()
	defer langMu.Unlock()
	_ = os.Unsetenv(EnvLangCLI)
	cliLanguageOverride = ""
	var value string
	for i := 0; i < len(arguments); i++ {
		arg := arguments[i]
		if arg == "--lang" && i+1 < len(arguments) {
			value = arguments[i+1]
			break
		}
		if strings.HasPrefix(arg, "--lang=") {
			value = strings.TrimPrefix(arg, "--lang=")
			break
		}
	}
	if contains(Languages, value) {
		cliLanguageOverride = value
		_ = os.Setenv(EnvLang, value)
		_ = os.Setenv(EnvLangCLI, "1")
	}
}

// BindConfigLanguage 把已校验配置中的 language 绑到进程内读取口.
func BindConfigLanguage(cfg *Config) {
	langMu.Lock()
	defer langMu.Unlock()
	if cfg == nil || !contains(Languages, cfg.Language) {
		configLanguage = ""
		return
	}
	configLanguage = cfg.Language
}

func explicitConfigLanguage(raw map[string]any) string {
	welcome, _ := raw["welcome_complete"].(bool)
	if !welcome {
		return ""
	}
	if _, exists := raw["language"]; !exists {
		return ""
	}
	language, _ := raw["language"].(string)
	if contains(Languages, language) {
		return language
	}
	return ""
}

// ConfiguredLanguage 返回有效 config.json 里显式保存的 language, 否则空字符串.
func ConfiguredLanguage() string {
	path, err := ConfigPath()
	if err != nil {
		return ""
	}
	data, err := readConfigBytes(path)
	if err != nil || data == nil {
		return ""
	}
	raw, err := decodeJSON(data)
	if err != nil {
		return ""
	}
	obj, ok := asObject(raw)
	if !ok {
		return ""
	}
	if _, err := Validate(obj); err != nil {
		return ""
	}
	return explicitConfigLanguage(obj)
}

func effectiveLocale() string {
	for _, name := range []string{EnvLang, "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// ResolveLanguage 优先级: --lang (KANDER_LANG_CLI) > 配置 > 环境变量; 默认 cn.
func ResolveLanguage() string {
	langMu.Lock()
	cli := cliLanguageOverride
	bound := configLanguage
	langMu.Unlock()
	if contains(Languages, cli) {
		return cli
	}
	if os.Getenv(EnvLangCLI) != "" {
		if lang := os.Getenv(EnvLang); contains(Languages, lang) {
			return lang
		}
	}
	if contains(Languages, bound) {
		return bound
	}
	locale := strings.ToLower(effectiveLocale())
	if strings.HasPrefix(locale, "en") {
		return "en"
	}
	return "cn"
}

// BindEffectiveLanguage 从磁盘配置绑定 language, 无效则清空.
func BindEffectiveLanguage() {
	language := ConfiguredLanguage()
	if language == "" {
		BindConfigLanguage(nil)
		return
	}
	BindConfigLanguage(&Config{Language: language})
}

func LanguageIsChinese() bool {
	return ResolveLanguage() == "cn"
}

// Text resolves a catalog message using the existing CLI/config/environment precedence.
func Text(id string, args ...any) string {
	return i18n.Text(ResolveLanguage(), id, args...)
}
