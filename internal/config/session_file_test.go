package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// The embedded pi definition declares the JSONL session-header format so a
// path-kind reported identity resolves to the session id inside the file.
func TestEmbeddedPiDeclaresSessionFile(t *testing.T) {
	agent, ok := embeddedByName("pi")
	if !ok {
		t.Fatal("pi definition missing")
	}
	file := agent.Session.File
	if file == nil {
		t.Fatal("pi must declare session.file")
	}
	if file.Format != "jsonl_header" || file.IDField != "id" || file.TypeField != "type" || file.TypeValue != "session" {
		t.Fatalf("file=%+v", file)
	}
}

// session.file validates the declared format, the id field, the paired type
// discriminator and the byte bound.
func TestSessionFileValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		file any
		bad  bool
	}{
		"valid minimal":    {file: map[string]any{"format": "jsonl_header", "id_field": "id"}},
		"valid full":       {file: map[string]any{"format": "jsonl_header", "id_field": "id", "type_field": "type", "type_value": "session", "max_bytes": 4096}},
		"unknown format":   {file: map[string]any{"format": "yaml", "id_field": "id"}, bad: true},
		"empty id_field":   {file: map[string]any{"format": "jsonl_header", "id_field": ""}, bad: true},
		"type unpaired":    {file: map[string]any{"format": "jsonl_header", "id_field": "id", "type_field": "type"}, bad: true},
		"value unpaired":   {file: map[string]any{"format": "jsonl_header", "id_field": "id", "type_value": "session"}, bad: true},
		"max_bytes over":   {file: map[string]any{"format": "jsonl_header", "id_field": "id", "max_bytes": MaxSessionFileMaxBytes + 1}, bad: true},
		"max_bytes minus":  {file: map[string]any{"format": "jsonl_header", "id_field": "id", "max_bytes": -1}, bad: true},
		"max_bytes zero":   {file: map[string]any{"format": "jsonl_header", "id_field": "id", "max_bytes": 0}},
		"no file is valid": {file: nil},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultConfig()
			session := map[string]any{"mode": "generated"}
			if tc.file != nil {
				session["file"] = tc.file
			}
			cfg.Agents = map[string]AgentDefinition{"custom": {
				Args: &AgentArgs{Start: []string{}, Resume: []string{}},
			}}
			data, _ := json.Marshal(cfg)
			var root map[string]any
			json.Unmarshal(data, &root)
			root["agents"].(map[string]any)["custom"].(map[string]any)["session"] = session
			data, _ = json.Marshal(root)
			_, err := ValidateJSON(data)
			if (err != nil) != tc.bad {
				t.Fatalf("err=%v bad=%v", err, tc.bad)
			}
			if tc.bad && err != nil && !strings.Contains(err.Error(), "session.file") {
				t.Fatalf("error must name session.file: %v", err)
			}
		})
	}
}

// cloneSession deep-copies the file declaration so overlays cannot alias the
// embedded or another definition's object.
func TestSessionFileClonesIndependently(t *testing.T) {
	src := &AgentSessionDefinition{Mode: "generated", File: &SessionFile{Format: "jsonl_header", IDField: "id"}}
	out := cloneSession(src)
	if out.File == src.File {
		t.Fatal("file declaration must be cloned")
	}
	out.File.IDField = "other"
	if src.File.IDField != "id" {
		t.Fatal("clone aliases the source")
	}
}
