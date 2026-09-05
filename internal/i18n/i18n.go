// Package i18n owns Kander's embedded message catalogs. Language selection stays
// with config; this package has no dependency on application configuration.
package i18n

import (
	"embed"
	"fmt"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var catalogs embed.FS

var localizers = loadLocalizers()

func loadLocalizers() map[string]*goi18n.Localizer {
	bundle := goi18n.NewBundle(language.SimplifiedChinese)
	for _, file := range []string{"locales/en.json", "locales/zh-CN.json"} {
		if _, err := bundle.LoadMessageFileFS(catalogs, file); err != nil {
			panic(fmt.Errorf("load embedded translations: %w", err))
		}
	}
	return map[string]*goi18n.Localizer{
		"cn": goi18n.NewLocalizer(bundle, "zh-CN"),
		"en": goi18n.NewLocalizer(bundle, "en"),
	}
}

// Text renders a message. cn remains the public Chinese language code; unknown
// languages fall back to Chinese. Arguments fill V0, V1, ... template variables.
// Missing IDs are returned verbatim so a catalog error cannot crash the CLI.
func Text(lang, id string, args ...any) string {
	if id == "" {
		return ""
	}
	localizer := localizers[lang]
	if localizer == nil {
		localizer = localizers["cn"]
	}
	var data map[string]any
	if len(args) > 0 {
		data = make(map[string]any, len(args))
		for index, value := range args {
			data[fmt.Sprintf("V%d", index)] = value
		}
	}
	text, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: id, TemplateData: data})
	if err != nil {
		return id
	}
	return text
}
