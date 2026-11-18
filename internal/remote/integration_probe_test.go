package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGitConfig keeps a test away from the developer's own user-level and
// system-level Git configuration, such as commit signing, global hooks and
// the default ignore file. The empty ignore file replaces the default one,
// which Git looks up below XDG_CONFIG_HOME or HOME. Package remote_test
// reaches it through IsolateGitConfig.
func isolateGitConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	ignore, config := filepath.Join(dir, "ignore"), filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(ignore, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("[core]\n\texcludesFile = "+filepath.ToSlash(ignore)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func probeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func TestRemoteServesAsksTheRemoteThroughTheClonesOwnOrigin(t *testing.T) {
	isolateGitConfig(t)
	root := t.TempDir()
	probeGit(t, root, "init", "-q", "--bare", "common.git")
	probeGit(t, root, "clone", "-q", "common.git", "work")
	work := filepath.Join(root, "work")
	// A relative origin resolves against the clone, never against a scratch directory.
	probeGit(t, work, "remote", "set-url", "origin", "../common.git")
	probeGit(t, work, "commit", "-q", "--allow-empty", "-m", "published")
	published := probeGit(t, work, "rev-parse", "HEAD")
	probeGit(t, work, "commit", "-q", "--allow-empty", "-m", "published tip")
	probeGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")
	probeGit(t, work, "commit", "-q", "--allow-empty", "-m", "local only")
	local := probeGit(t, work, "rev-parse", "HEAD")
	if err := remoteServes(t.Context(), work, "../common.git", published); err != nil {
		t.Fatalf("a commit below the remote tip: %v", err)
	}
	if err := remoteServes(t.Context(), work, "../common.git", local); err == nil {
		t.Fatal("a commit only the clone holds counted as published")
	}
}

func TestRemoteServesFallsBackWhenNegotiationIsUnavailable(t *testing.T) {
	isolateGitConfig(t)
	// Negotiation needs protocol version 2; version 0 must still prove the remote.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.version")
	t.Setenv("GIT_CONFIG_VALUE_0", "0")
	root := t.TempDir()
	probeGit(t, root, "init", "-q", "--bare", "common.git")
	probeGit(t, root, "clone", "-q", "common.git", "work")
	work := filepath.Join(root, "work")
	probeGit(t, work, "remote", "set-url", "origin", "../common.git")
	probeGit(t, work, "commit", "-q", "--allow-empty", "-m", "published")
	published := probeGit(t, work, "rev-parse", "HEAD")
	probeGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")
	probeGit(t, work, "commit", "-q", "--allow-empty", "-m", "local only")
	local := probeGit(t, work, "rev-parse", "HEAD")
	if err := remoteServes(t.Context(), work, "../common.git", published); err != nil {
		t.Fatalf("a published commit under protocol 0: %v", err)
	}
	if err := remoteServes(t.Context(), work, "../common.git", local); err == nil {
		t.Fatal("a commit only the clone holds counted as published under protocol 0")
	}
}
