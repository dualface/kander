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

// ApplyLanguageArgument scans argv for --lang and sets KANDER_LANG / KANDER_LANG_CLI.
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

// BindConfigLanguage binds the language of a validated config to the in-process accessor.
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

// ConfiguredLanguage returns the language explicitly saved in a valid config.json, otherwise an empty string.
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

// ResolveLanguage resolves in order: --lang (KANDER_LANG_CLI) > config > environment; the default is cn.
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

// BindEffectiveLanguage binds the language from the on-disk config, clearing it when invalid.
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
