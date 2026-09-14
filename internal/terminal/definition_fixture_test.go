package terminal

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal/terminaltest"
)

func TestMain(m *testing.M) {
	terminaltest.Main()
	os.Exit(m.Run())
}

func resetLanguage(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
}

// fixtureDefinition is a complete third-party style definition exercising
// every operation with the three parse primitives.
const fixtureDefinition = `{
  "schema_version": 1,
  "name": "faketerm",
  "binary": "faketerm",
  "version_args": ["--version"],
  "capabilities": {"container": true, "focus": true, "pane_metadata": true, "foreground_process": true, "wait_output": true, "session_report": false},
  "address": [{"name": "container"}, {"name": "pane"}],
  "errors": {
    "gone": ["exit:3", "stderr:(?i)no such pane", "stdout_json:error.code=pane_not_found"],
    "meta_missing": ["exit:4"]
  },
  "launchers": {
    "faketerm": {
      "auto_priority": 10,
      "requires": {"platform": "any", "binary": true, "inside_session": true, "env": [{"name": "FAKETERM"}]},
      "started_lines": [{"message": "{head} pane={pane}"}]
    }
  },
  "ops": {
    "create_container": {
      "steps": [{
        "store": "new",
        "argv": ["new", "--cwd", "{cwd}", "--label", "{label}"],
        "output": {"source": "stdout", "parse": "json_field:result.id"},
        "fields": {"container": "regex:^([^/]+)/", "pane": "regex:/(.+)$"}
      }],
      "result": {"container": "{step.new.container}", "pane": "{step.new.pane}"}
    },
    "wait_ready": {"steps": [{"argv": ["ready", "{pane}"], "poll": {"interval": "5ms", "timeout": "30s", "until": "nonempty"}}]},
    "run_command": {"steps": [{"argv": ["run", "{pane}", "{command}"]}]},
    "set_session_marker": {"steps": [{"argv": ["meta", "set", "{pane}", "{value}"]}]},
    "pane_facts": {
      "steps": [
        {
          "store": "facts",
          "argv": ["facts", "{pane}"],
          "output": {"source": "stdout", "parse": "raw"},
          "fields": {"command": "json_field:command", "dead": "json_field:dead", "in_mode": "json_field:mode"}
        },
        {"store": "marker", "argv": ["meta", "get", "{pane}"], "on_error": "meta_missing", "output": {"source": "stdout", "parse": "regex:^(\\S+)"}}
      ],
      "result": {"command": "{step.facts.command}", "dead": "{step.facts.dead}", "in_mode": "{step.facts.in_mode}", "session_marker": "{step.marker.text}"}
    },
    "read_output": {"steps": [{"store": "out", "argv": ["read", "{pane}"]}], "result": {"text": "{step.out.text}"}},
    "wait_output": {"steps": [{"argv": ["read", "{pane}"], "poll": {"interval": "5ms", "timeout": "{timeout_ms}", "until": "matched"}}]},
    "deliver_text": {"steps": [{"argv": ["type", "{pane}", "{text}"]}, {"argv": ["key", "{pane}", "Enter"]}]},
    "topology": {
      "steps": [{
        "store": "topo",
        "argv": ["topology", "{pane}"],
        "output": {"source": "stdout", "parse": "raw"},
        "fields": {"container": "json_field:container", "count": "json_field:count"}
      }],
      "result": {"container": "{step.topo.container}", "pane_count": "{step.topo.count}"}
    },
    "container_exists": {"steps": [{"argv": ["exists", "{container}"]}]},
    "reverse_lookup": {
      "steps": [{"store": "list", "argv": ["list"]}],
      "rows": {
        "from": "list",
        "fields": {"container": "regex:^(\\S+) ", "pane": "regex:^\\S+ (\\S+) ", "marker": "regex:^\\S+ \\S+ (\\S+) ", "command": "regex:(\\S+)$"},
        "expect": "!field_missing:row.pane",
        "match": [["field:row.marker={reference}", "field:row.command={process_name}"]],
        "result": {"container": "{row.container}", "pane": "{row.pane}"}
      }
    },
    "focus": {"closed_when": "field:facts.dead=1", "steps": [{"argv": ["focus", "{container}", "{pane}"]}]},
    "close_container": {"steps": [{"argv": ["close", "{container}"]}]}
  }
}`

// mutateFixture decodes the fixture into generic JSON, applies a change and
// re-encodes it, so validation cases state only their difference.
func mutateFixture(t *testing.T, change func(root map[string]any)) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(fixtureDefinition), &root); err != nil {
		t.Fatal(err)
	}
	change(root)
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func object(value any, path ...string) map[string]any {
	current := value.(map[string]any)
	for _, key := range path {
		current = current[key].(map[string]any)
	}
	return current
}

func firstStep(root map[string]any, op string) map[string]any {
	return object(root, "ops", op)["steps"].([]any)[0].(map[string]any)
}

func fixtureBackend(t *testing.T, getenv func(string) string) *DeclarativeBackend {
	t.Helper()
	def, err := DecodeDefinition("faketerm.json", []byte(fixtureDefinition))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewDeclarativeBackend(def, "faketerm", getenv)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func jsonUnmarshal(text string, target any) error {
	return json.Unmarshal([]byte(text), target)
}
