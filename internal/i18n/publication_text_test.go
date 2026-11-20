package i18n

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The two texts that send a holder to confirm a joint publication name the command that reads the
// material, and say that the confirmation binds the task revision, the execution generation and
// the digest of the material, not only the first two.
func TestPublicationConfirmationTextsNameTheReadCommandAndTheBinding(t *testing.T) {
	binding := map[string][]string{
		"en":    {"revision", "execution generation", "digest"},
		"ja":    {"リビジョン", "実行世代", "ダイジェスト"},
		"zh-CN": {"修订号", "执行代次", "材料摘要"},
	}
	oldPhrase := map[string]string{
		"en":    "limited to this task revision and execution generation",
		"ja":    "承認はこのタスクのリビジョンと実行世代に限定されます",
		"zh-CN": "确认仅绑定本卡版本及当前执行代次",
	}
	for language, words := range binding {
		data, err := os.ReadFile("locales/server/" + language + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"remote.publication_confirmation_request", "remote.review_confirmation_pending"} {
			text := catalog[key]
			for _, want := range append([]string{"kander review publication <absolute-CWD> {{.V0}}", "kander review confirm <absolute-CWD> {{.V0}} "}, words...) {
				if !strings.Contains(text, want) {
					t.Errorf("%s %s omits %q: %s", language, key, want, text)
				}
			}
			if strings.Contains(text, oldPhrase[language]) {
				t.Errorf("%s %s keeps the old wording: %s", language, key, text)
			}
		}
	}
}
