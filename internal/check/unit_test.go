package check

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestEscapeControls(t *testing.T) {
	got := escapeRunes("a" + string(rune(0x1b)) + "b" + string(rune(0x9b)) + "c\u2028d")
	if got != `a\x1bb\u009bc\u2028d` {
		t.Fatalf("escapeRunes=%q", got)
	}
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\u009b") || strings.Contains(got, "\u2028") {
		t.Fatalf("raw control remained: %q", got)
	}
}

func TestGitCommandEnv(t *testing.T) {
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANGUAGE", "zh_CN")
	joined := strings.Join(gitCommandEnv(), "\n")
	for _, want := range []string{"GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "LANG=C"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "zh_CN") {
		t.Fatalf("host locale leaked: %s", joined)
	}
}

func TestPhysicalLines(t *testing.T) {
	tests := []struct {
		name string
		blob []byte
		want int
	}{
		{"empty", nil, 0},
		{"empty slice", []byte{}, 0},
		{"no newline", []byte("a"), 1},
		{"single newline", []byte("\n"), 1},
		{"terminated", []byte("a\n"), 1},
		{"two terminated", []byte("a\nb\n"), 2},
		{"two missing final", []byte("a\nb"), 2},
		{"1000 terminated", bytes.Repeat([]byte("x\n"), 1000), 1000},
		{"1001 terminated", bytes.Repeat([]byte("x\n"), 1001), 1001},
		{"1001 missing final", append(bytes.Repeat([]byte("x\n"), 1000), 'y'), 1001},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := physicalLines(tc.blob); got != tc.want {
				t.Fatalf("physicalLines=%d want %d", got, tc.want)
			}
		})
	}
}

func TestGitPathRoundTrip(t *testing.T) {
	utf8Path := gitPath([]byte("dir/file name.txt"))
	data, err := json.Marshal(utf8Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"utf8":"dir/file name.txt"`)) || !bytes.Contains(data, []byte(`"base64":null`)) {
		t.Fatalf("utf8 encoding: %s", data)
	}
	var decoded GitPath
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Raw, utf8Path.Raw) {
		t.Fatalf("utf8 round trip %q", decoded.Raw)
	}

	raw := []byte{0xff, 0xfe, '/', 'a'}
	encoded := gitPath(raw)
	data, err = json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	wantB64 := base64.StdEncoding.EncodeToString(raw)
	if !bytes.Contains(data, []byte(`"utf8":null`)) || !bytes.Contains(data, []byte(`"base64":"`+wantB64+`"`)) {
		t.Fatalf("base64 encoding: %s", data)
	}
	decoded = GitPath{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Raw, raw) {
		t.Fatalf("base64 round trip %q", decoded.Raw)
	}
}

func TestGitPathSpecialBytes(t *testing.T) {
	samples := [][]byte{
		[]byte("file name.txt"),
		[]byte("a\tb.txt"),
		[]byte("a\nb.txt"),
		[]byte(`quote"file.txt`),
		[]byte(`back\slash.txt`),
		[]byte("-dash.txt"),
	}
	for _, raw := range samples {
		data, err := json.Marshal(gitPath(raw))
		if err != nil {
			t.Fatal(err)
		}
		var decoded GitPath
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%q: %v %s", raw, err, data)
		}
		if !bytes.Equal(decoded.Raw, raw) {
			t.Fatalf("%q round trip %q", raw, decoded.Raw)
		}
	}
}

func TestParseNameStatus(t *testing.T) {
	in := []byte("A\x00added.txt\x00M\x00mod.txt\x00D\x00gone.txt\x00R100\x00old.txt\x00new.txt\x00C075\x00src.txt\x00copy.txt\x00")
	got, err := parseNameStatus(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].letter != 'A' || string(got[0].path) != "added.txt" || got[0].oldPath != nil {
		t.Fatalf("add: %+v", got[0])
	}
	if got[3].letter != 'R' || string(got[3].oldPath) != "old.txt" || string(got[3].path) != "new.txt" {
		t.Fatalf("rename: %+v", got[3])
	}
	if got[4].letter != 'C' || string(got[4].oldPath) != "src.txt" || string(got[4].path) != "copy.txt" {
		t.Fatalf("copy: %+v", got[4])
	}
	if _, err := parseNameStatus([]byte("A\x00truncated")); err == nil {
		t.Fatal("expected truncated error")
	}
}

func TestParseTreeEntry(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantType   string
		wantObject string
		wantOK     bool
	}{
		{name: "blob", data: []byte("100644 blob abc123\tpath\twith-tab\x00"), wantType: "blob", wantObject: "abc123", wantOK: true},
		{name: "gitlink", data: []byte("160000 commit def456\tmodule\x00"), wantType: "commit", wantObject: "def456", wantOK: true},
		{name: "empty", data: nil},
		{name: "missing terminator", data: []byte("100644 blob abc123\tpath")},
		{name: "multiple", data: []byte("100644 blob abc123\ta\x00100644 blob def456\tb\x00")},
		{name: "missing tab", data: []byte("100644 blob abc123\x00")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objectType, objectID, ok := parseTreeEntry(tc.data)
			if objectType != tc.wantType || objectID != tc.wantObject || ok != tc.wantOK {
				t.Fatalf("parseTreeEntry=%q,%q,%t want %q,%q,%t", objectType, objectID, ok, tc.wantType, tc.wantObject, tc.wantOK)
			}
		})
	}
}

func TestDeliveryStatusPrecedence(t *testing.T) {
	if got := deliveryStatus(true, true); got != statusFail {
		t.Fatalf("fail+candidates=%s", got)
	}
	if got := deliveryStatus(false, true); got != statusReviewRequired {
		t.Fatalf("candidates=%s", got)
	}
	if got := deliveryStatus(false, false); got != statusPass {
		t.Fatalf("clean=%s", got)
	}
}

func TestEscapeBytes(t *testing.T) {
	got := escapeBytes([]byte("a\n\x1b[31m\tb"))
	if got != `a\n\x1b[31m\tb` {
		t.Fatalf("got %q", got)
	}
	if bytes.Contains([]byte(got), []byte{'\n'}) || bytes.Contains([]byte(got), []byte{0x1b}) {
		t.Fatal("escaped display still contains control bytes")
	}
	invalid := escapeBytes([]byte{0xff, '\n', 0x01})
	if bytes.Contains([]byte(invalid), []byte{'\n'}) || bytes.Contains([]byte(invalid), []byte{0xff}) {
		t.Fatalf("invalid utf8 display %q", invalid)
	}
}
