package svcinstall

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The specs below are the definitions kander service install writes: the worker with a client
// home set, the server without one, and a Homebrew entry on macOS (the symlink, never its
// Cellar target).
var (
	linuxWorker = Spec{Kind: KindWorker, Executable: "/home/alice/.local/bin/kander", Args: []string{"worker"},
		ClientHome: "/srv/kander client", WorkDir: "/srv/kander client", LogPath: "/srv/kander client/worker.log"}
	linuxServer = Spec{Kind: KindServer, Executable: "/home/alice/.local/bin/kander",
		Args:    []string{"server", "start", "--foreground", "--data", "/home/alice/.local/share/kander-server", "--listen", "127.0.0.1:7878", "--ttl", "1m30s", "--renew", "30s"},
		WorkDir: "/home/alice/.local/share/kander-server", LogPath: "/home/alice/.local/share/kander-server/server.log"}
	macWorker = Spec{Kind: KindWorker, Executable: "/opt/homebrew/bin/kander", Args: []string{"worker"},
		ClientHome: "/Users/alice/kander-client", WorkDir: "/Users/alice/kander-client", LogPath: "/Users/alice/kander-client/worker.log"}
	macServer = Spec{Kind: KindServer, Executable: "/opt/homebrew/bin/kander",
		Args:    []string{"server", "start", "--foreground", "--data", "/Users/alice/.local/share/kander-server", "--listen", "127.0.0.1:7878", "--ttl", "1m30s", "--renew", "30s"},
		WorkDir: "/Users/alice/.local/share/kander-server", LogPath: "/Users/alice/.local/share/kander-server/server.log"}
	windowsWorker = Spec{Kind: KindWorker, Executable: `C:\Users\alice\.local\bin\kander.exe`, Args: []string{"worker"},
		ClientHome: `D:\kander client`, WorkDir: `D:\kander client`, LogPath: `D:\kander client\worker.log`}
	windowsServer = Spec{Kind: KindServer, Executable: `C:\Users\alice\.local\bin\kander.exe`,
		Args:    []string{"server", "start", "--foreground", "--data", `C:\Users\alice\.local\share\kander-server`, "--listen", "127.0.0.1:7878", "--ttl", "1m30s", "--renew", "30s"},
		WorkDir: `C:\Users\alice\.local\share\kander-server`, LogPath: `C:\Users\alice\.local\share\kander-server\server.log`}
)

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s differs; run go test -run Golden -update\n--- got\n%s", name, got)
	}
}

func TestGoldenDefinitions(t *testing.T) {
	cases := []struct {
		name string
		p    platform
		spec Spec
		// must are the lines that keep the agents of a worker alive and the service resident.
		must []string
	}{
		{"systemd_worker.service.golden", systemd{dir: "/u"}, linuxWorker, []string{"Restart=always", "KillMode=process", `Environment="KANDER_MANAGED=1"`, `Environment="KANDER_CLIENT_HOME=/srv/kander client"`}},
		{"systemd_server.service.golden", systemd{dir: "/u"}, linuxServer, []string{"Restart=always", `Environment="KANDER_MANAGED=1"`, `"--data" "/home/alice/.local/share/kander-server"`}},
		{"launchd_worker.plist.golden", launchd{dir: "/l", uid: "501"}, macWorker, []string{"<key>KeepAlive</key>\n\t<true/>", "<key>AbandonProcessGroup</key>\n\t<true/>", "<string>/opt/homebrew/bin/kander</string>", "<key>KANDER_CLIENT_HOME</key>"}},
		{"launchd_server.plist.golden", launchd{dir: "/l", uid: "501"}, macServer, []string{"<key>KeepAlive</key>", "<key>AbandonProcessGroup</key>", "<key>KANDER_MANAGED</key>"}},
		{"schtasks_worker.xml.golden", schtasks{user: `HOST\alice`}, windowsWorker, []string{"<LogonTrigger>", "<RestartOnFailure>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", `set "KANDER_MANAGED=1"`, `set "KANDER_CLIENT_HOME=D:\kander client"`}},
		{"schtasks_server.xml.golden", schtasks{user: `HOST\alice`}, windowsServer, []string{"<LogonTrigger>", "<RestartOnFailure>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.p.render(c.spec)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, c.name, got)
			for _, line := range c.must {
				if !strings.Contains(string(got), line) {
					t.Fatalf("%s lacks %q", c.name, line)
				}
			}
			m, ok := decodeMarker(got)
			if !ok || !sameMarker(m, marker{Schema: markerSchema, Spec: c.spec}) {
				t.Fatalf("the marker does not give the spec back: %+v %v", m, ok)
			}
		})
	}
}

// A definition without a client home sets none; the server keeps the default kill mode.
func TestDefinitionsWithoutClientHome(t *testing.T) {
	for _, p := range []platform{systemd{dir: "/u"}, launchd{dir: "/l", uid: "501"}, schtasks{user: "alice"}} {
		spec := linuxServer
		if p.name() == "schtasks" {
			spec = windowsServer
		}
		got, err := p.render(spec)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "KANDER_CLIENT_HOME") {
			t.Fatalf("%s: a definition without a client home names one", p.name())
		}
		if !strings.Contains(string(got), "KANDER_MANAGED") {
			t.Fatalf("%s: no managed mode", p.name())
		}
	}
	unit, _ := systemd{dir: "/u"}.render(linuxServer)
	if strings.Contains(string(unit), "KillMode") {
		t.Fatal("the server unit keeps the default kill mode")
	}
}

func TestMarkerIsPlainASCIIWithoutHyphens(t *testing.T) {
	spec := linuxWorker
	spec.ClientHome = "/home/愛/k--x"
	line, err := encodeMarker(spec)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(line, markerPrefix)
	for _, r := range body {
		if r > 0x7f || r == '-' {
			t.Fatalf("the marker carries %q: %s", r, line)
		}
	}
	m, ok := decodeMarker([]byte("<!-- " + line + " -->"))
	if !ok || m.ClientHome != spec.ClientHome {
		t.Fatalf("decoded %+v %v", m, ok)
	}
}

func TestRenderRefusesValuesTheDefinitionCannotCarry(t *testing.T) {
	bad := linuxWorker
	bad.ClientHome = "/x\ny"
	if _, err := (systemd{}).render(bad); err == nil {
		t.Fatal("a newline was accepted")
	}
	relative := linuxWorker
	relative.Executable = "kander"
	if _, err := (launchd{}).render(relative); err == nil {
		t.Fatal("a relative executable was accepted")
	}
	for _, value := range []string{`D:\50%`, `D:\a"b`, `D:\dir\`} {
		w := windowsWorker
		w.ClientHome = value
		if _, err := (schtasks{user: "a"}).render(w); err == nil {
			t.Fatalf("schtasks accepted %q", value)
		}
	}
}

func TestSystemdQuotesSpecifiersAndVariables(t *testing.T) {
	spec := linuxWorker
	spec.Executable = `/opt/a%b$c "d"\e/kander`
	got, err := systemd{}.render(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `ExecStart="/opt/a%%b$$c \"d\"\\e/kander" "worker"`) {
		t.Fatalf("ExecStart is not quoted:\n%s", got)
	}
}
