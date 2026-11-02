package board

import (
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// A takeover of 1.0.0 left the start attempt pending under a card that names the
// new executor. Adoption refuses with a localized error that names the takeover,
// and the attempt stays pending. A card outside working and review gets the same.
func TestOldTakenOverPendingRefusesAdoption(t *testing.T) {
	root := tempBoard(t)
	s := legacyStartedCard(t, root, "legacy-taken-over")
	id := s.Entry.TaskID
	legacyAttempt(t, root, s, "stuck-attempt", "pending", 0)
	if err := writeLegacyTakeover(root, transactionSnapshot(t, root, id), "claude", "claude 1.0.0-takeover", "tmux:$1:@1:%1"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	if _, err := BindExecutionCursor(root, s.Entry); err != nil {
		t.Fatal(err)
	}
	_, err := AdoptExecution(root, s, "user")
	var e *Error
	if !errors.As(err, &e) || e.Code != "board.adopt_start_identity_changed" || !strings.Contains(e.Message, "--agent") || !strings.Contains(e.Message, "--decision") {
		t.Fatalf("err=%v", err)
	}
	if r := cursorRecord(t, root, id); r.Status != ExecutionPending {
		t.Fatalf("record %+v", r)
	}
	matching := cursorRecord(t, root, id)
	matching.Owner, matching.Session = MetadataFrom(s.Text, FieldOwner), MetadataFrom(s.Text, FieldSession)
	if err = CheckAdoptable(s, matching); err != nil {
		t.Fatalf("a matching identity in working: %v", err)
	}
	done := s
	done.Entry.State = "done"
	if err = CheckAdoptable(done, matching); !errors.As(err, &e) || e.Code != "board.adopt_start_identity_changed" {
		t.Fatalf("err=%v", err)
	}
}

// A member reclaimed without a handoff never reconciles by itself. Its reason keeps
// the fixed phrase and names the exit after the user's decision, in every language.
func TestMissingHandoffBlockNamesTheRecoverExit(t *testing.T) {
	for lang, ask := range map[string]string{"en": "ask the user", "cn": "先请示用户", "ja": "まずユーザーに報告して判断を仰ぎ"} {
		t.Run(lang, func(t *testing.T) {
			root := tempBoard(t)
			t.Setenv(config.EnvLang, lang)
			config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
			ids, c := blockedGroup(t, root)
			reason := c.Members[ids[0]].Blocked.Reason
			for _, want := range []string{"execution cycle changed without lifecycle handoff", "kander coordinator recover", ask} {
				if !strings.Contains(reason, want) {
					t.Fatalf("reason=%q, want %q", reason, want)
				}
			}
			if strings.Index(reason, "kander coordinator recover") < strings.Index(reason, ask) {
				t.Fatalf("reason=%q names recover before asking the user", reason)
			}
		})
	}
}
