package terminalcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal"
)

const fixtureEnv = "KANDER_CONFORMANCE_FAKE"

// TestMain also serves a stateful fake terminal executable. Each command is
// a fresh process, so file state proves real creation, input and cleanup.
func TestMain(m *testing.M) {
	if root := os.Getenv(fixtureEnv); root != "" && len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		os.Exit(serveFake(root, os.Args[1:]))
	}
	os.Exit(m.Run())
}
func serveFake(root string, args []string) int {
	write := func(name, value string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			panic(err)
		}
	}
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(root, name)); return string(b) }
	log, _ := os.OpenFile(filepath.Join(root, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if log != nil {
		fmt.Fprintln(log, strings.Join(args, "|"))
		log.Close()
	}
	if read("closed") == "yes" && (args[0] == "facts" || args[0] == "exists") {
		fmt.Fprintln(os.Stderr, "pane missing")
		return 3
	}
	if read("failure") == args[0] {
		fmt.Fprintln(os.Stderr, "intentional failure")
		return 9
	}
	switch args[0] {
	case "version":
		fmt.Println("fake 1")
	case "create":
		write("cwd", args[2])
		write("created", "yes")
		fmt.Print("c1 p1")
	case "ready":
		fmt.Println("ready")
	case "run":
		nonce := regexp.MustCompile(`[0-9a-f]{24}`).FindString(args[2])
		write("nonce", nonce)
		write("output", "kander-process-"+nonce+":sh\nkander-ready-"+nonce)
	case "meta-set":
		write("marker", args[2])
	case "meta-get":
		if read("wrong") == "yes" {
			fmt.Println("wrong-value")
		} else {
			fmt.Println(read("marker"))
		}
	case "facts":
		count, _ := strconv.Atoi(read("facts-count"))
		count++
		write("facts-count", strconv.Itoa(count))
		if read("failure") == "foreground" && count >= 2 {
			fmt.Fprintln(os.Stderr, "intentional foreground failure")
			return 9
		}
		fmt.Println(`{"command":"sh","dead":"0","mode":"0"}`)
	case "read":
		fmt.Println(read("output"))
	case "wait":
		if !strings.Contains(read("output"), args[2]) {
			return 4
		}
	case "type":
		write("pending", args[2])
	case "key":
		if args[2] != "Enter" {
			return 5
		}
		write("output", read("output")+"\nkander-ack-"+read("pending"))
	case "topology":
		fmt.Print("c1 1")
	case "lookup":
		fmt.Println("c1 p1 " + read("marker"))
	case "close":
		write("closed", "yes")
	case "focus":
		fmt.Println("focused")
	case "exists":
	default:
		fmt.Fprintln(os.Stderr, "unknown fake operation")
		return 8
	}
	return 0
}

const fakeDefinition = `{
 "schema_version":1,"name":"checkterm","binary":"placeholder","version_args":["version"],
 "capabilities":{"container":true,"focus":true,"pane_metadata":true,"foreground_process":true,"wait_output":true},
 "address":[{"name":"container"},{"name":"pane"}],"errors":{"gone":["exit:3"]},
 "launchers":{"checkterm":{"requires":{"platform":"any","binary":true}}},
 "ops":{
 "create_container":{"steps":[{"store":"new","argv":["create","{label}","{cwd}"],"fields":{"container":"regex:^(\\S+)","pane":"regex:(\\S+)$"}}],"result":{"container":"{step.new.container}","pane":"{step.new.pane}"}},
 "wait_ready":{"steps":[{"argv":["ready","{pane}"]}]},
 "run_command":{"steps":[{"argv":["run","{pane}","{command}"]}]},
 "set_session_marker":{"steps":[{"argv":["meta-set","{pane}","{value}"]}]},
 "pane_facts":{"steps":[{"store":"facts","argv":["facts","{pane}"],"fields":{"command":"json_field:command","dead":"json_field:dead","mode":"json_field:mode"}},{"store":"marker","argv":["meta-get","{pane}"],"output":{"source":"stdout","parse":"regex:^(.*)"}}],"result":{"command":"{step.facts.command}","dead":"{step.facts.dead}","in_mode":"{step.facts.mode}","session_marker":"{step.marker.text}"}},
 "read_output":{"steps":[{"store":"read","argv":["read","{pane}"]}],"result":{"text":"{step.read.text}"}},
 "wait_output":{"steps":[{"argv":["wait","{pane}","{marker}"]}]},
 "deliver_text":{"steps":[{"argv":["type","{pane}","{text}"]},{"argv":["key","{pane}","Enter"]}]},
 "topology":{"steps":[{"store":"topology","argv":["topology","{pane}"],"fields":{"container":"regex:^(\\S+)","count":"regex:(\\S+)$"}}],"result":{"container":"{step.topology.container}","pane_count":"{step.topology.count}"}},
 "container_exists":{"steps":[{"argv":["exists","{container}"]}]},
 "reverse_lookup":{"steps":[{"store":"list","argv":["lookup"]}],"rows":{"from":"list","fields":{"pane":"regex:^\\S+ (\\S+)","marker":"regex:^\\S+ \\S+ (.*)$"},"match":"field:row.marker={reference}","result":{"container":"c1","pane":"{row.pane}"}}},
 "focus":{"steps":[{"argv":["focus","{pane}"]}]},
 "close_container":{"steps":[{"argv":["close","{container}"]}]}
 }} `

func fixture(t *testing.T, capability string) (terminal.Backend, string) {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		if data, err := os.ReadFile(filepath.Join(root, "cwd")); err == nil {
			if err := os.RemoveAll(string(data)); err != nil {
				t.Error(err)
			}
		}
	})
	t.Setenv(fixtureEnv, root)
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	var def map[string]any
	if err := json.Unmarshal([]byte(fakeDefinition), &def); err != nil {
		t.Fatal(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	def["binary"] = program
	if capability != "" {
		def["capabilities"].(map[string]any)[capability] = false
		ops := def["ops"].(map[string]any)
		facts := ops["pane_facts"].(map[string]any)
		result := facts["result"].(map[string]any)
		switch capability {
		case "pane_metadata":
			delete(ops, "set_session_marker")
			delete(result, "session_marker")
			facts["steps"] = facts["steps"].([]any)[:1]
		case "foreground_process":
			delete(result, "command")
			delete(result, "dead")
			delete(result, "in_mode")
		}
	}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	share := t.TempDir()
	dir := filepath.Join(share, "terminals")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checkterm.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir {
		return []terminal.DefinitionDir{{Source: terminal.SourceProject, Root: share, Path: dir}}
	}
	terminal.ReloadDefinitions()
	t.Cleanup(func() { terminal.DefinitionDirs = saved; terminal.ReloadDefinitions() })
	backend, err := selectBackend("checkterm")
	if err != nil {
		t.Fatal(err)
	}
	return backend, root
}
func writeFixture(t *testing.T, root, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
