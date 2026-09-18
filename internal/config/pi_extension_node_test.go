package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestPiExtensionLogicUnderNode exercises the embedded TypeScript extension's
// exported decision functions under the same runtime pi loads it with. The
// harness imports the real embedded source, so a logic regression fails the
// test instead of drifting into a shipped file.
func TestPiExtensionLogicUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to exercise the pi extension source")
	}
	_, source, ok := AgentExtension("pi")
	if !ok {
		t.Fatal("pi extension missing")
	}
	dir := t.TempDir()
	ext := filepath.Join(dir, "kander-rules.ts")
	if err := os.WriteFile(ext, source, 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness.mjs")
	if err := os.WriteFile(harness, []byte(nodeHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, harness)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, out)
	}
}

const nodeHarness = `import * as assert from "node:assert/strict";
import * as ext from "./kander-rules.ts";

// Context-file gate: only a loaded file mentioning the entry triggers injection.
assert.equal(ext.contextReferencesKander([{ path: "/x/AGENTS.md", content: "read KANDER-AGENTS.md first" }]), true);
assert.equal(ext.contextReferencesKander([{ path: "/x/AGENTS.md", content: "no reference" }]), false);
assert.equal(ext.contextReferencesKander([]), false);
assert.equal(ext.contextReferencesKander(undefined), false);

// Rules root resolution: cwd project rules win, then the main worktree via the
// git common dir, then the global root; nothing found resolves to null.
const files = new Set(["/proj/.kander/rules/KANDER-AGENTS.md", "/main/.kander/rules/KANDER-AGENTS.md", "/home/.agents/kander/KANDER-AGENTS.md"]);
const deps = {
  exists: (p) => files.has(p),
  homedir: () => "/home",
  gitCommonDir: (cwd) => cwd === "/proj" || cwd === "/main/wt" ? "/main/.git" : null,
};
assert.deepEqual(ext.resolveRulesRoot("/proj", deps), { root: "/proj/.kander/rules", scope: "project" });
assert.deepEqual(ext.resolveRulesRoot("/main/wt", deps), { root: "/main/.kander/rules", scope: "project" });
files.delete("/proj/.kander/rules/KANDER-AGENTS.md");
files.delete("/main/.kander/rules/KANDER-AGENTS.md");
assert.deepEqual(ext.resolveRulesRoot("/elsewhere", deps), { root: "/home/.agents/kander", scope: "global" });
files.delete("/home/.agents/kander/KANDER-AGENTS.md");
assert.equal(ext.resolveRulesRoot("/elsewhere", deps), null);

// Injection block: every rule file, the absolute paths, the rules root, and
// the still-required config probe are named; a missing file yields no block.
const read = (p) => ({ "/r/KANDER-AGENTS.md": "# A", "/r/KANDER-BASE-RULES.md": "# B", "/r/KANDER-LOADING-RULES.md": "# L" })[p] ?? (() => { throw new Error("missing"); })();
const block = ext.buildInjection("/r", { read });
assert.ok(block.includes("/r/KANDER-AGENTS.md"));
assert.ok(block.includes("/r/KANDER-BASE-RULES.md"));
assert.ok(block.includes("/r/KANDER-LOADING-RULES.md"));
assert.ok(block.includes("Rules root: /r"));
assert.ok(block.includes("kander config --json"));
assert.equal(ext.buildInjection("/r", { read: () => { throw new Error("x"); } }), null);

// Switches: /kander-rules off and PI_KANDER_RULES=0 disable injection; the
// context reference alone cannot re-enable them.
const state = { enabled: true };
const ctx = [{ path: "/x/AGENTS.md", content: "KANDER-AGENTS.md" }];
assert.equal(ext.shouldInject(state, {}, ctx), true);
assert.equal(ext.applyCommand(state, "off"), "off");
assert.equal(ext.shouldInject(state, {}, ctx), false);
assert.equal(ext.applyCommand(state, "on"), "on");
assert.equal(ext.shouldInject(state, { PI_KANDER_RULES: "0" }, ctx), false);
assert.equal(ext.applyCommand(state, "status"), "status");
assert.equal(ext.applyCommand(state, "bogus"), "usage");
assert.ok(ext.statusText(state, {}, { root: "/r", scope: "global" }).includes("/r"));

console.log("kander-rules extension harness: ok");
`
