package config

import (
	"testing"
)

// An overlay session that keeps the embedded mode inherits the embedded
// session.file declaration it predates.
func TestAgentForSessionFileInheritedOnSameMode(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{
		"pi": {Session: &AgentSessionDefinition{Mode: "generated"}},
	}}
	d := AgentFor(cfg, "pi")
	if d.Session.File == nil {
		t.Fatal("session.file must be inherited from the embedded pi definition")
	}
	embedded, _ := embeddedByName("pi")
	if *d.Session.File != *embedded.Session.File {
		t.Fatalf("inherited file=%+v want %+v", d.Session.File, embedded.Session.File)
	}
}

// An explicit overlay session.file always wins over the embedded declaration.
func TestAgentForSessionFileOverlayWins(t *testing.T) {
	own := &SessionFile{Format: "jsonl_header", IDField: "custom_id"}
	cfg := &Config{Agents: map[string]AgentDefinition{
		"pi": {Session: &AgentSessionDefinition{Mode: "generated", File: own}},
	}}
	d := AgentFor(cfg, "pi")
	if d.Session.File == nil || d.Session.File.IDField != "custom_id" {
		t.Fatalf("overlay file lost: %+v", d.Session.File)
	}
}

// An overlay session whose mode differs from the embedded mode opts out of
// the file inheritance.
func TestAgentForSessionFileNotInheritedOnDifferentMode(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{
		"pi": {Session: &AgentSessionDefinition{Mode: "none"}},
	}}
	d := AgentFor(cfg, "pi")
	if d.Session.File != nil {
		t.Fatalf("different mode must not inherit: %+v", d.Session.File)
	}
}

// An overlay without a session keeps the existing whole-session inheritance,
// which already carries the file declaration.
func TestAgentForSessionFileWholeSessionInherit(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{
		"pi": {Path: "/usr/local/bin/pi"},
	}}
	d := AgentFor(cfg, "pi")
	if d.Session == nil || d.Session.File == nil || d.Session.File.Format != "jsonl_header" {
		t.Fatalf("whole-session inheritance lost the file: %+v", d.Session)
	}
}

// A wrapper agent driving the pi CLI through dialect inherits the same file
// declaration: the session file format follows the CLI, not the agent name.
func TestAgentForSessionFileInheritedByDialectWrapper(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{
		"piwrap": {Dialect: "pi", Session: &AgentSessionDefinition{Mode: "generated"}},
	}}
	d := AgentFor(cfg, "piwrap")
	if d.Session.File == nil || d.Session.File.Format != "jsonl_header" {
		t.Fatalf("dialect wrapper must inherit: %+v", d.Session)
	}
	embedded, _ := embeddedByName("pi")
	if *d.Session.File != *embedded.Session.File {
		t.Fatalf("inherited file=%+v", d.Session.File)
	}
}

// The inherited declaration is a detached copy: mutating the resolved
// definition touches neither the embedded definition nor the raw overlay, and
// the raw overlay stays byte-identical so save paths cannot write the
// inherited value back.
func TestAgentForSessionFileInheritDetached(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDefinition{
		"pi": {Session: &AgentSessionDefinition{Mode: "generated"}},
	}}
	d := AgentFor(cfg, "pi")
	if d.Session.File == nil {
		t.Fatal("no inherited file")
	}
	embedded, _ := embeddedByName("pi")
	if d.Session.File == embedded.Session.File {
		t.Fatal("inherited file aliases the embedded declaration")
	}
	d.Session.File.IDField = "mutated"
	if embedded.Session.File.IDField == "mutated" {
		t.Fatal("embedded declaration mutated through the resolved definition")
	}
	raw := cfg.Agents["pi"]
	if raw.Session.File != nil {
		t.Fatal("raw overlay rewritten by AgentFor")
	}
	d2 := AgentFor(cfg, "pi")
	if d2.Session.File == nil || d2.Session.File.IDField != embedded.Session.File.IDField {
		t.Fatalf("second resolve polluted: %+v", d2.Session.File)
	}
}
