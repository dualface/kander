package config

import "testing"

func TestServiceBindingSurvivesBeforeWelcome(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WelcomeComplete = false
	cfg.Project.Name = "before-welcome"
	effective, err := Effective(cfg)
	if err != nil || effective.Project.Name != cfg.Project.Name || effective.Server.URL != "" || ServerURL(effective) != DefaultServerURL {
		t.Fatalf("effective=%+v err=%v", effective, err)
	}
	cfg.Server.URL = "http://127.0.0.1:1"
	effective, err = Effective(cfg)
	if err != nil || ServerURL(effective) != cfg.Server.URL {
		t.Fatalf("explicit binding=%+v %v", effective, err)
	}
}
