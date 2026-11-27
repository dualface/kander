package remote

import (
	"strings"
	"testing"
)

func TestClientHost(t *testing.T) {
	for _, tc := range []struct {
		id, want string
	}{
		{"c-42f1216e5b1c-center5900", "center5900"},
		{"c-42f1216e5b1c-my-host", "my-host"},
		{"c-42f1216e5b1c", ""},
		{"c-42f1216e5b1c-", ""},
		{"", ""},
		{"x-42f1216e5b1c-center5900", ""},
		{"c-42f1216e5b1-center5900", ""},
		{"c-42F1216E5B1C-center5900", ""},
		{"c-42f1216e5b1cz-center5900", ""},
	} {
		if got := ClientHost(tc.id); got != tc.want {
			t.Errorf("ClientHost(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestClientHostReadsGeneratedID(t *testing.T) {
	id, err := ClientID(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimPrefix(id[len("c-")+clientIDRandomLength:], "-")
	if host := ClientHost(id); host != want {
		t.Fatalf("ClientHost(%q) = %q, want %q", id, host, want)
	}
}
