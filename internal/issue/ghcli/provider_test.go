package ghcli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/issue/ghcli/ghclitest"
)

type fakeResponse struct {
	stdout string
	stderr string
	err    error
}

type fakeRunner struct {
	responses   map[string]fakeResponse
	calls       []string
	directories []string
}

func (f *fakeRunner) Run(_ context.Context, directory string, argv []string, _ int64) ([]byte, []byte, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	f.directories = append(f.directories, directory)
	response, ok := f.responses[key]
	if !ok {
		return nil, nil, errors.New("unexpected command: " + key)
	}
	return []byte(response.stdout), []byte(response.stderr), response.err
}

const (
	gitConfigKey     = "config --local --list"
	repoViewKey      = "repo view github.com/dualface/kander --json nameWithOwner,url,isPrivate"
	defaultRepoJSON  = `{"isPrivate":true,"nameWithOwner":"dualface/kander","url":"https://github.com/dualface/kander"}`
	remoteOriginOnly = "core.bare=false\nremote.origin.url=https://github.com/dualface/kander.git\nremote.origin.fetch=+refs/heads/*:refs/remotes/origin/*\n"
)

func newTestProvider(t *testing.T, gh, git map[string]fakeResponse, env map[string]string) (*Provider, *fakeRunner, *fakeRunner) {
	t.Helper()
	ghRunner := &fakeRunner{responses: gh}
	gitRunner := &fakeRunner{responses: git}
	provider := NewProvider(Options{
		Gh:  ghRunner,
		Git: gitRunner,
		Env: func(key string) string { return env[key] },
	})
	return provider, ghRunner, gitRunner
}

func TestResolveRepositoryFromSingleRemote(t *testing.T) {
	provider, ghRunner, _ := newTestProvider(t,
		map[string]fakeResponse{repoViewKey: {stdout: defaultRepoJSON}},
		map[string]fakeResponse{gitConfigKey: {stdout: remoteOriginOnly}},
		nil,
	)
	repository, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := issue.Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Private: true, Remote: "origin",
	}
	if repository != want {
		t.Fatalf("repository=%+v want %+v", repository, want)
	}
	if len(ghRunner.calls) != 1 {
		t.Fatalf("gh calls=%v", ghRunner.calls)
	}
}

func TestResolveRepositoryNeverGuessesBetweenRemotes(t *testing.T) {
	git := map[string]fakeResponse{gitConfigKey: {stdout: "remote.origin.url=https://github.com/dualface/kander.git\nremote.upstream.url=https://github.com/cli/cli.git\n"}}
	provider, ghRunner, _ := newTestProvider(t, map[string]fakeResponse{}, git, nil)
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if issue.KindOf(err) != issue.ErrorAmbiguousRemotes {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if len(ghRunner.calls) != 0 {
		t.Fatalf("an ambiguous repository must not reach the provider: %v", ghRunner.calls)
	}
	structured := err.(*issue.Error)
	joined := strings.Join(structured.Candidates, " ")
	for _, want := range []string{"dualface/kander (origin)", "cli/cli (upstream)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("candidates %q missing %q", joined, want)
		}
	}
}

func TestResolveRepositoryHonorsSetDefaultRemote(t *testing.T) {
	git := map[string]fakeResponse{gitConfigKey: {stdout: "remote.origin.url=https://github.com/dualface/kander.git\nremote.upstream.url=https://github.com/cli/cli.git\nremote.upstream.gh-resolved=base\n"}}
	gh := map[string]fakeResponse{"repo view github.com/cli/cli --json nameWithOwner,url,isPrivate": {stdout: `{"isPrivate":false,"nameWithOwner":"cli/cli","url":"https://github.com/cli/cli"}`}}
	provider, _, _ := newTestProvider(t, gh, git, nil)
	repository, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repository.Owner != "cli" || repository.Name != "cli" || repository.Remote != "upstream" {
		t.Fatalf("repository=%+v", repository)
	}
}

func TestResolveRepositoryDeduplicatesEquivalentRemotes(t *testing.T) {
	git := map[string]fakeResponse{gitConfigKey: {stdout: "remote.origin.url=https://github.com/dualface/kander.git\nremote.mirror.url=git@github.com:dualface/kander.git\n"}}
	provider, _, _ := newTestProvider(t,
		map[string]fakeResponse{repoViewKey: {stdout: defaultRepoJSON}},
		git,
		nil,
	)
	repository, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repository.Owner != "dualface" || repository.Name != "kander" {
		t.Fatalf("repository=%+v", repository)
	}
}

func TestResolveRepositoryHonorsEnvironmentOverride(t *testing.T) {
	provider, _, gitRunner := newTestProvider(t,
		map[string]fakeResponse{"repo view acme/tool --json nameWithOwner,url,isPrivate": {stdout: `{"isPrivate":false,"nameWithOwner":"acme/tool","url":"https://github.com/acme/tool"}`}},
		map[string]fakeResponse{},
		map[string]string{EnvRepo: "acme/tool"},
	)
	repository, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repository.Owner != "acme" || repository.Name != "tool" || repository.Remote != "" {
		t.Fatalf("repository=%+v", repository)
	}
	if len(gitRunner.calls) != 0 {
		t.Fatalf("explicit environment override must not probe remotes: %v", gitRunner.calls)
	}
}

func TestResolveRepositoryHonorsEnvironmentHost(t *testing.T) {
	provider, _, _ := newTestProvider(t,
		map[string]fakeResponse{"repo view ghe.example.com/acme/tool --json nameWithOwner,url,isPrivate": {stdout: `{"isPrivate":true,"nameWithOwner":"acme/tool","url":"https://ghe.example.com/acme/tool"}`}},
		map[string]fakeResponse{},
		map[string]string{EnvRepo: "acme/tool", EnvHost: "ghe.example.com"},
	)
	repository, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repository.Host != "ghe.example.com" {
		t.Fatalf("host=%q", repository.Host)
	}
}

func TestResolveRepositoryRejectsHostMismatch(t *testing.T) {
	provider, _, _ := newTestProvider(t,
		map[string]fakeResponse{"repo view ghe.example.com/acme/tool --json nameWithOwner,url,isPrivate": {stdout: `{"isPrivate":true,"nameWithOwner":"acme/tool","url":"https://github.com/acme/tool"}`}},
		map[string]fakeResponse{},
		nil,
	)
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "ghe.example.com/acme/tool")
	if issue.KindOf(err) != issue.ErrorInvalidResponse {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestResolveRepositoryRejectsCredentialBearingRemote(t *testing.T) {
	const secret = "sup3rsecret"
	git := map[string]fakeResponse{gitConfigKey: {stdout: "remote.origin.url=https://user:" + secret + "@github.com/dualface/kander.git\n"}}
	provider, ghRunner, _ := newTestProvider(t, map[string]fakeResponse{}, git, nil)
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if issue.KindOf(err) != issue.ErrorInsecureRemote {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the credential: %v", err)
	}
	if len(ghRunner.calls) != 0 {
		t.Fatalf("credential-bearing remote must not reach the provider: %v", ghRunner.calls)
	}
}

func TestResolveRepositoryNotAGitRepository(t *testing.T) {
	git := map[string]fakeResponse{gitConfigKey: {
		stderr: "fatal: --local can only be used inside a git repository",
		err:    issue.NewError(issue.ErrorCommandFailed, "git", "fatal: --local can only be used inside a git repository"),
	}}
	provider, _, _ := newTestProvider(t, map[string]fakeResponse{}, git, nil)
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if issue.KindOf(err) != issue.ErrorNotRepository {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestResolveRepositoryWithoutRemote(t *testing.T) {
	provider, _, _ := newTestProvider(t,
		map[string]fakeResponse{},
		map[string]fakeResponse{gitConfigKey: {stdout: "core.bare=false\nremote.origin.fetch=+refs/heads/*:refs/remotes/origin/*\n"}},
		nil,
	)
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if issue.KindOf(err) != issue.ErrorNoRemote {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}

func TestResolveRepositoryRejectsInvalidReferenceBeforeRunningCommands(t *testing.T) {
	provider, ghRunner, gitRunner := newTestProvider(t, map[string]fakeResponse{}, map[string]fakeResponse{}, nil)
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "../etc/passwd")
	if issue.KindOf(err) != issue.ErrorInvalidReference {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
	if len(ghRunner.calls) != 0 || len(gitRunner.calls) != 0 {
		t.Fatalf("invalid reference ran commands: gh=%v git=%v", ghRunner.calls, gitRunner.calls)
	}
}

func TestResolveRepositoryValidatesProviderResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "not json", body: "not json"},
		{name: "missing nameWithOwner", body: `{"url":"https://github.com/dualface/kander","isPrivate":false}`},
		{name: "missing isPrivate", body: `{"nameWithOwner":"dualface/kander","url":"https://github.com/dualface/kander"}`},
		{name: "url mismatch", body: `{"nameWithOwner":"dualface/kander","url":"https://github.com/other/repo","isPrivate":false}`},
		{name: "not https", body: `{"nameWithOwner":"dualface/kander","url":"http://github.com/dualface/kander","isPrivate":false}`},
		{name: "credential url", body: `{"nameWithOwner":"dualface/kander","url":"https://user:secret@github.com/dualface/kander","isPrivate":false}`},
		{name: "other repository", body: `{"nameWithOwner":"other/repo","url":"https://github.com/other/repo","isPrivate":false}`},
		{name: "non utf8", body: string([]byte{0xff, 0xfe})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, _, _ := newTestProvider(t,
				map[string]fakeResponse{repoViewKey: {stdout: test.body}},
				map[string]fakeResponse{gitConfigKey: {stdout: remoteOriginOnly}},
				nil,
			)
			_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
			if issue.KindOf(err) != issue.ErrorInvalidResponse {
				t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
			}
		})
	}
}

func TestClassifyRepositoryFailure(t *testing.T) {
	ref := issue.RepositoryRef{Host: "github.com", Owner: "dualface", Name: "kander"}
	tests := []struct {
		name   string
		stderr string
		kind   issue.ErrorKind
	}{
		{name: "unauthenticated", stderr: "To get started with GitHub CLI, please run: gh auth login", kind: issue.ErrorUnauthenticated},
		{name: "not logged in", stderr: "not logged in to github.com. use 'gh auth login'", kind: issue.ErrorUnauthenticated},
		{name: "sso", stderr: "Resource protected by organization SAML enforcement", kind: issue.ErrorSSORequired},
		{name: "rate limited", stderr: "API rate limit exceeded for user ID 1", kind: issue.ErrorRateLimited},
		{name: "forbidden", stderr: "HTTP 403: Resource not accessible by integration", kind: issue.ErrorUnauthorized},
		{name: "not found", stderr: "HTTP 404: Not Found", kind: issue.ErrorNotFound},
		{name: "ambiguous", stderr: "no default repository has been set; use `gh repo set-default` to select one", kind: issue.ErrorAmbiguousRemotes},
		{name: "generic", stderr: "something went wrong", kind: issue.ErrorCommandFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := classifyRepositoryFailure([]byte(test.stderr), errors.New("exit status 1"), ref)
			if err.Kind != test.kind {
				t.Fatalf("kind=%q want %q (%v)", err.Kind, test.kind, err)
			}
			if test.kind == issue.ErrorNotFound && !strings.Contains(err.Detail, "dualface/kander") {
				t.Fatalf("not-found detail=%q", err.Detail)
			}
			if test.kind != issue.ErrorNotFound && strings.Contains(err.Detail, "auth login") && strings.Contains(err.Detail, "gho_") {
				t.Fatalf("detail leaks credentials: %q", err.Detail)
			}
		})
	}
}

func TestProviderEndToEndThroughFakeCLI(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "git", ghclitest.Options{Stdout: remoteOriginOnly})
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: defaultRepoJSON})
	record := t.TempDir() + "/calls.jsonl"
	t.Setenv(ghclitest.RecordEnv, record)

	workdir := t.TempDir()
	provider := NewProvider(Options{})
	repository, err := provider.ResolveRepository(context.Background(), workdir, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repository.Host != "github.com" || repository.Owner != "dualface" || repository.Name != "kander" || !repository.Private || repository.Remote != "origin" {
		t.Fatalf("repository=%+v", repository)
	}
	invocations := ghclitest.Record(t, record)
	if len(invocations) != 2 {
		t.Fatalf("invocations=%d want 2 (%v)", len(invocations), invocations)
	}
}

func TestProviderEndToEndEnvironmentOverrideOutsideWorktree(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "git", ghclitest.Options{Stderr: "fatal: not a git repository", Exit: 128})
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: `{"isPrivate":false,"nameWithOwner":"acme/tool","url":"https://ghe.example.com/acme/tool"}`})
	t.Setenv(EnvRepo, "ghe.example.com/acme/tool")

	workdir := t.TempDir()
	repository, err := NewProvider(Options{}).ResolveRepository(context.Background(), workdir, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repository.Host != "ghe.example.com" || repository.Owner != "acme" || repository.Name != "tool" {
		t.Fatalf("repository=%+v", repository)
	}
}

func TestProviderEndToEndRejectsOversizedResponse(t *testing.T) {
	ghclitest.Install(t)
	ghclitest.Set(t, "git", ghclitest.Options{Stdout: remoteOriginOnly})
	ghclitest.Set(t, "gh", ghclitest.Options{Stdout: strings.Repeat("x", 4096), Repeat: 512})
	provider := NewProvider(Options{StdoutLimit: 2048, Gh: &ExecRunner{Program: "gh", StdoutLimit: 2048}})
	_, err := provider.ResolveRepository(context.Background(), t.TempDir(), "")
	if issue.KindOf(err) != issue.ErrorOutputLimit {
		t.Fatalf("kind=%q err=%v", issue.KindOf(err), err)
	}
}
