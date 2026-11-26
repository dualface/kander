package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/version"
)

func TestNewDescriptionFileAndListRequestFlag(t *testing.T) {
	t.Setenv(EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Chdir(t.TempDir())

	const name = "requests"
	id := api.ProjectID(name)
	posts := make(chan map[string]json.RawMessage, 4)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: version.String()})
		case r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode(api.Project{ID: id, Name: name})
		case r.URL.Path == "/api/v1/projects/"+id+"/tasks" && r.Method == http.MethodPost:
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case posts <- body:
			default:
				t.Error("unexpected repeated creation request")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(api.Task{TaskID: "20261126-request-task", State: "backlog", Revision: 1})
		case r.URL.Path == "/api/v1/projects/"+id+"/tasks" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(api.TaskList{Tasks: []api.TaskSummary{
				{TaskID: "20261126-request-task", Request: true},
				{TaskID: "20261126-ordinary-task"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hs.Close)
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	cfg.Server.URL, cfg.Project.Name = hs.URL, name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	run := func(flags ...string) (int, string, string) {
		return commandtest.Capture(func() int {
			return commandtest.Run(NewNewCommand(), append([]string{"feature", "request", "Request", "--json"}, flags...)...)
		})
	}
	readPost := func() map[string]json.RawMessage {
		t.Helper()
		select {
		case body := <-posts:
			return body
		default:
			t.Fatal("creation command did not send a request")
			return nil
		}
	}

	if code, out, stderr := run(); code != 0 {
		t.Fatalf("new without file: exit=%d out=%q stderr=%q", code, out, stderr)
	}
	if _, present := readPost()["description"]; present {
		t.Fatal("ordinary creation sent the description key")
	}
	path := filepath.Join(t.TempDir(), "description.md")
	const original = " 描述\r\n\tsecond line "
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, stderr := run("--description-file", path); code != 0 {
		t.Fatalf("new with file: exit=%d out=%q stderr=%q", code, out, stderr)
	}
	var sent string
	if err := json.Unmarshal(readPost()["description"], &sent); err != nil || sent != original {
		t.Fatalf("description sent = %q, %v", sent, err)
	}

	invalidFile := filepath.Join(t.TempDir(), "input.md")
	for _, tc := range []struct{ name, content string }{
		{"missing", ""},
		{"invalid-utf8", "text\xff"},
		{"empty", ""},
		{"blank", "\t \r\n\u3000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name != "missing" {
				if err := os.WriteFile(invalidFile, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if code, out, stderr := run("--description-file", invalidFile); code == 0 || out != "" || stderr == "" {
				t.Fatalf("invalid input: exit=%d out=%q stderr=%q", code, out, stderr)
			}
			select {
			case <-posts:
				t.Fatal("invalid input sent a creation request")
			default:
			}
		})
	}

	code, out, stderr := commandtest.Capture(func() int { return commandtest.Run(NewListCommand(nil), "--json") })
	var list struct {
		Tasks []struct {
			TaskID  string `json:"task_id"`
			Request *bool  `json:"request"`
		}
	}
	if err := json.Unmarshal([]byte(out), &list); code != 0 || err != nil || len(list.Tasks) != 2 {
		t.Fatalf("list: exit=%d out=%q stderr=%q err=%v", code, out, stderr, err)
	}
	want := map[string]bool{"20261126-request-task": true, "20261126-ordinary-task": false}
	for _, task := range list.Tasks {
		value, ok := want[task.TaskID]
		if !ok || task.Request == nil || *task.Request != value {
			t.Fatalf("missing or incorrect request flag: %s", out)
		}
	}
}
