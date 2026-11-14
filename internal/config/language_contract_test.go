package config

import (
	"strings"
	"testing"
)

func TestLanguageConstraint(t *testing.T) {
	for value, want := range map[string]string{
		"zh-CN": "本任务所有相关会话的硬性约束：使用 zh-CN 语言与用户交流。",
		"ja":    "本タスクに関連するすべての会話の必須要件：ja でユーザーとやり取りしてください。",
		"en":    `Hard requirement for every conversation related to this task: communicate with the user in "en".`,
		"fr":    `Hard requirement for every conversation related to this task: communicate with the user in "fr".`,
	} {
		if got := LanguageConstraint(value); got != want {
			t.Errorf("%q: got %q, want %q", value, got, want)
		}
	}
	for _, value := range []string{"", "a\nb", "ja\x1b[0m", strings.Repeat("x", 65)} {
		if got := LanguageConstraint(value); got != "" {
			t.Errorf("invalid %q: %q", value, got)
		}
	}
}
