package menu

import (
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestAdvanceLanguageKeepsDirtyTabBaseline(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	base := session.CaptureLanguage()
	session.SetLanguage("ja")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.SetLanguage("cn")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	if !session.ScopeDirty || session.OverlayDirty {
		t.Fatalf("setup: scope dirty=%v overlay dirty=%v", session.ScopeDirty, session.OverlayDirty)
	}

	base = session.AdvanceLanguage(base)
	if err := session.RestoreLanguage(base); err != nil {
		t.Fatal(err)
	}
	if session.Config.Language != "cn" || !session.FieldOverridden("language") {
		t.Fatalf("saved overlay language restored away: %s", session.Config.Language)
	}
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	if session.Config.Language != "en" {
		t.Fatalf("unsaved scope language=%s want en", session.Config.Language)
	}
}
