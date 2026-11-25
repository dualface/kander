package api

import "testing"

func TestSharedGitIdentityRejectsCredentialsAndExecutableHelpers(t *testing.T) {
	for _, remote := range []string{"https://user:password@example.invalid/repo", "https://token@example.invalid/repo", "ssh://git:password@example.invalid/repo", "user:password@example.invalid:repo", "https://example.invalid/repo?token=secret", "ext::sh -c command", "--upload-pack=command", "git@example.invalid:one\ngit@example.invalid:two"} {
		if SafeGitRemote(remote) {
			t.Fatalf("unsafe shared identity accepted: %q", remote)
		}
	}
	for _, remote := range []string{"git@example.invalid:repo", "ssh://git@example.invalid/repo", "https://example.invalid/repo", "/tmp/common.git", "file:///tmp/common.git"} {
		if !SafeGitRemote(remote) {
			t.Fatalf("credential-free identity rejected: %q", remote)
		}
	}
}
