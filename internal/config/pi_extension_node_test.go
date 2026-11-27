package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestPiExtensionLogicUnderNode exercises the embedded TypeScript extension's
// exported decision functions under a vanilla node harness: the test imports
// the real embedded source, so a logic regression fails the test instead of
// drifting into a shipped file.
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
import * as path from "node:path";
import * as ext from "./kander-rules.ts";

// The extension joins paths natively, so expected paths use path.join too.
const j = (...parts) => path.join(...parts);

// Context-file gate: only a loaded file mentioning the entry triggers injection.
assert.equal(ext.contextReferencesKander([{ path: "/x/AGENTS.md", content: "read KANDER-AGENTS.md first" }]), true);
assert.equal(ext.contextReferencesKander([{ path: "/x/AGENTS.md", content: "no reference" }]), false);
assert.equal(ext.contextReferencesKander([]), false);
assert.equal(ext.contextReferencesKander(undefined), false);

// Rules root resolution: only the global root counts, whatever a checkout
// holds; nothing found resolves to null.
const files = new Set([j("/home/.agents/kander/KANDER-AGENTS.md"), j("/proj/.kander/rules/KANDER-AGENTS.md")]);
const deps = { exists: (p) => files.has(p), homedir: () => "/home" };
assert.deepEqual(ext.resolveRulesRoot(deps), { root: j("/home/.agents/kander") });
files.delete(j("/home/.agents/kander/KANDER-AGENTS.md"));
assert.equal(ext.resolveRulesRoot(deps), null);

// Injection block: every rule file, the absolute paths, the rules root, and
// the still-required config probe are named; a missing file yields no block.
const read = (p) => ({ [j("/r/KANDER-AGENTS.md")]: "# A", [j("/r/KANDER-BASE-RULES.md")]: "# B", [j("/r/KANDER-LOADING-RULES.md")]: "# L" })[p] ?? (() => { throw new Error("missing"); })();
const block = ext.buildInjection("/r", { read });
assert.ok(block.includes(j("/r/KANDER-AGENTS.md")));
assert.ok(block.includes(j("/r/KANDER-BASE-RULES.md")));
assert.ok(block.includes(j("/r/KANDER-LOADING-RULES.md")));
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
assert.ok(ext.statusText(state, {}, { root: "/r" }).includes("/r"));

console.log("kander-rules extension harness: ok");
`
