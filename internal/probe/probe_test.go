package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
)

func TestContextBudgetDefaultsAndInheritance(t *testing.T) {
	for _, duration := range []time.Duration{0, 25 * time.Millisecond, 30 * time.Second} {
		parent := context.Background()
		parentCancel := func() {}
		if duration != 0 {
			parent, parentCancel = context.WithTimeout(parent, duration)
		}
		ctx, cancel := WithDefaultTimeout(parent)
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("missing default deadline")
		}
		if duration == 0 {
			if remaining := time.Until(deadline); remaining <= 0 || remaining > DefaultCommandTimeout {
				t.Fatalf("remaining=%s", remaining)
			}
		} else if inherited, _ := parent.Deadline(); deadline != inherited {
			t.Fatalf("deadline reset: %s != %s", deadline, inherited)
		}
		cancel()
		parentCancel()
	}
}

func TestCancellationMessagesInAllLanguages(t *testing.T) {
	for _, test := range []struct{ language, deadline, canceled string }{
		{"cn", "探测期限已耗尽", "探测已取消"},
		{"en", "Probe deadline exhausted", "Probe canceled"},
		{"ja", "プローブの期限を超過", "プローブをキャンセル"},
	} {
		t.Run(test.language, func(t *testing.T) {
			t.Setenv(config.EnvLang, test.language)
			t.Setenv(config.EnvLangCLI, "1")
			config.ApplyLanguageArgument([]string{"kander", "--lang", test.language})
			defer config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
			for _, item := range []struct {
				err  error
				want string
			}{{context.DeadlineExceeded, test.deadline}, {context.Canceled, test.canceled}} {
				detail := FailureDetail(item.err)
				if !strings.Contains(detail, item.want) || !strings.Contains(detail, item.err.Error()) {
					t.Fatalf("detail=%q", detail)
				}
			}
		})
	}
}
