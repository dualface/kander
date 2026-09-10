package issue

import (
	"strings"
	"testing"
)

func TestParseRepositoryRef(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		host    string
		owner   string
		repo    string
		wantErr bool
	}{
		{name: "owner and repository", input: "dualface/kander", owner: "dualface", repo: "kander"},
		{name: "host owner repository", input: "github.com/dualface/kander", host: "github.com", owner: "dualface", repo: "kander"},
		{name: "host lowercased", input: "GitHub.Example.COM/acme/tool", host: "github.example.com", owner: "acme", repo: "tool"},
		{name: "trailing git suffix stripped", input: "dualface/kander.git", owner: "dualface", repo: "kander"},
		{name: "enterprise host with port", input: "ghe.example.com:8443/acme/tool", host: "ghe.example.com:8443", owner: "acme", repo: "tool"},
		{name: "surrounding spaces trimmed", input: "  dualface/kander  ", owner: "dualface", repo: "kander"},
		{name: "underscore and dot segments", input: "acme_labs/tool.fork", owner: "acme_labs", repo: "tool.fork"},
		{name: "empty", input: "", wantErr: true},
		{name: "only spaces", input: "   ", wantErr: true},
		{name: "one segment", input: "kander", wantErr: true},
		{name: "four segments", input: "github.com/org/team/repo", wantErr: true},
		{name: "traversal", input: "../../etc/passwd", wantErr: true},
		{name: "control character", input: "dualface/ka\x1bnder", wantErr: true},
		{name: "invalid host label", input: "-bad.example.com/acme/tool", wantErr: true},
		{name: "host with scheme", input: "https://github.com/acme/tool", wantErr: true},
		{name: "credential host", input: "user:pass@github.com/acme/tool", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := ParseRepositoryRef(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", test.input)
				}
				if KindOf(err) != ErrorInvalidReference {
					t.Fatalf("kind=%q want %q", KindOf(err), ErrorInvalidReference)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Host != test.host || ref.Owner != test.owner || ref.Name != test.repo {
				t.Fatalf("ref=%+v want host=%q owner=%q repo=%q", ref, test.host, test.owner, test.repo)
			}
		})
	}
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		host  string
		owner string
		repo  string
		ok    bool
	}{
		{name: "https github", input: "https://github.com/dualface/kander.git", host: "github.com", owner: "dualface", repo: "kander", ok: true},
		{name: "https without suffix", input: "https://github.com/dualface/kander", host: "github.com", owner: "dualface", repo: "kander", ok: true},
		{name: "scp like", input: "git@github.com:dualface/kander.git", host: "github.com", owner: "dualface", repo: "kander", ok: true},
		{name: "ssh enterprise", input: "ssh://git@ghe.example.com/acme/tool.git", host: "ghe.example.com", owner: "acme", repo: "tool", ok: true},
		{name: "https user info without password", input: "https://git@github.com/acme/tool.git", host: "github.com", owner: "acme", repo: "tool", ok: true},
		{name: "local path", input: "/srv/git/repo.git", ok: false},
		{name: "file url", input: "file:///srv/git/repo.git", ok: false},
		{name: "single segment", input: "https://heroku.com/app.git", ok: false},
		{name: "three path segments", input: "https://gitlab.com/group/subgroup/repo.git", ok: false},
		{name: "tree url", input: "https://github.com/dualface/kander/tree/main", ok: false},
		{name: "scp like without repository", input: "git@github.com:dualface", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, ok, err := ParseRemoteURL(test.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != test.ok {
				t.Fatalf("ok=%v want %v (ref=%+v)", ok, test.ok, ref)
			}
			if !ok {
				return
			}
			if ref.Host != test.host || ref.Owner != test.owner || ref.Name != test.repo {
				t.Fatalf("ref=%+v want host=%q owner=%q repo=%q", ref, test.host, test.owner, test.repo)
			}
		})
	}
}

func TestParseRemoteURLRejectsEmbeddedCredentials(t *testing.T) {
	const secret = "sup3rsecret"
	for _, input := range []string{
		"https://user:" + secret + "@github.com/dualface/kander.git",
		"user:" + secret + "@github.com:dualface/kander.git",
	} {
		_, ok, err := ParseRemoteURL(input)
		if ok {
			t.Fatalf("credential-bearing URL accepted: %q", input)
		}
		if err == nil {
			t.Fatalf("expected insecure remote error for %q", input)
		}
		if KindOf(err) != ErrorInsecureRemote {
			t.Fatalf("kind=%q want %q", KindOf(err), ErrorInsecureRemote)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks the credential: %v", err)
		}
	}
}

func TestRepositoryReference(t *testing.T) {
	repository := Repository{Host: "github.com", Owner: "dualface", Name: "kander"}
	if got, want := repository.FullName(), "dualface/kander"; got != want {
		t.Fatalf("FullName=%q want %q", got, want)
	}
	if got, want := repository.Reference(), "github.com/dualface/kander"; got != want {
		t.Fatalf("Reference=%q want %q", got, want)
	}
	if got, want := (Repository{Owner: "dualface", Name: "kander"}).Reference(), "dualface/kander"; got != want {
		t.Fatalf("hostless Reference=%q want %q", got, want)
	}
}
