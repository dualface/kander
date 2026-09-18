/**
 * Kander Rules Extension
 * kander-extension-version: 1
 *
 * Installed by `kander install` / `kander doctor --repair` into pi's extension
 * directory. Injects KANDER-AGENTS.md, KANDER-BASE-RULES.md and
 * KANDER-LOADING-RULES.md into the system prompt whenever a loaded context
 * file (AGENTS.md and friends) references KANDER-AGENTS.md, so a pi session
 * holds the Kander rules on the first turn instead of relying on the model
 * to read them.
 *
 * Rule files are re-read every turn, so rules upgrades take effect without
 * restarting pi. Toggle for this session with `/kander-rules on|off|status`;
 * disable for the whole process with PI_KANDER_RULES=0.
 */

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { execFileSync } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export const KANDER_RULES_EXTENSION_VERSION = "1";
export const KANDER_RULE_FILES = [
	"KANDER-AGENTS.md",
	"KANDER-BASE-RULES.md",
	"KANDER-LOADING-RULES.md",
] as const;
const ENTRY_FILE = "KANDER-AGENTS.md";

export interface ContextFile {
	path: string;
	content: string;
}

export interface ExtensionState {
	enabled: boolean;
}

export interface RootDeps {
	exists?: (target: string) => boolean;
	homedir?: () => string;
	gitCommonDir?: (cwd: string) => string | null;
}

export interface ResolvedRulesRoot {
	root: string;
	scope: "project" | "global";
}

/** True when any loaded context file mentions the Kander rules entry. */
export function contextReferencesKander(contextFiles: readonly ContextFile[] | undefined | null): boolean {
	if (!contextFiles) {
		return false;
	}
	return contextFiles.some(
		(file) => typeof file?.content === "string" && file.content.includes(ENTRY_FILE),
	);
}

/** True when the process environment disables the extension. */
export function envDisabled(env: NodeJS.ProcessEnv): boolean {
	return env.PI_KANDER_RULES === "0";
}

/** True when this turn should inject: extension on, env on, context references the entry. */
export function shouldInject(
	state: ExtensionState,
	env: NodeJS.ProcessEnv,
	contextFiles: readonly ContextFile[] | undefined | null,
): boolean {
	return state.enabled && !envDisabled(env) && contextReferencesKander(contextFiles);
}

function defaultGitCommonDir(cwd: string): string | null {
	try {
		const out = execFileSync(
			"git",
			["rev-parse", "--path-format=absolute", "--git-common-dir"],
			{ cwd, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"], timeout: 5000 },
		).trim();
		return out === "" ? null : out;
	} catch {
		return null;
	}
}

/**
 * Resolve the Kander rules root for a working directory: the project rules
 * root (<main worktree>/.kander/rules/) wins, then the global rules root
 * (~/.agents/kander/). A directory counts only when the entry file exists.
 * The git common dir makes linked worktrees resolve to the main worktree;
 * when git is unavailable or the cwd is not a repository, only the cwd and
 * the global root are tried.
 */
export function resolveRulesRoot(cwd: string, deps: RootDeps = {}): ResolvedRulesRoot | null {
	const exists = deps.exists ?? ((target: string) => fs.existsSync(target));
	const hasEntry = (root: string) => exists(path.join(root, ENTRY_FILE));

	const cwdRules = path.join(cwd, ".kander", "rules");
	if (hasEntry(cwdRules)) {
		return { root: cwdRules, scope: "project" };
	}
	const common = (deps.gitCommonDir ?? defaultGitCommonDir)(cwd);
	if (common) {
		const mainRules = path.join(path.dirname(common), ".kander", "rules");
		if (mainRules !== cwdRules && hasEntry(mainRules)) {
			return { root: mainRules, scope: "project" };
		}
	}
	const home = (deps.homedir ?? os.homedir)();
	const globalRules = path.join(home, ".agents", "kander");
	if (hasEntry(globalRules)) {
		return { root: globalRules, scope: "global" };
	}
	return null;
}

/**
 * Build the injected system-prompt block from a rules root, or null when a
 * required rule file is missing or empty. Only the fixed file names are read.
 */
export function buildInjection(
	rulesRoot: string,
	deps: { read?: (target: string) => string } = {},
): string | null {
	const read = deps.read ?? ((target: string) => fs.readFileSync(target, "utf8"));
	const sections: string[] = [];
	for (const name of KANDER_RULE_FILES) {
		const file = path.join(rulesRoot, name);
		let content: string;
		try {
			content = read(file);
		} catch {
			return null;
		}
		if (typeof content !== "string" || content.trim() === "") {
			return null;
		}
		sections.push(`### ${file}\n\n${content.trimEnd()}`);
	}
	return [
		"## Kander Workflow Rules",
		"",
		`Rules root: ${rulesRoot}`,
		"",
		"The Kander rule files below are injected in full by the kander-rules pi extension.",
		"Follow them as the Kander workflow rules entry: run `kander config --json` for the",
		"current scope first, then load the enabled rule modules named by",
		"KANDER-LOADING-RULES.md.",
		"",
		...sections,
	].join("\n");
}

/**
 * Apply a /kander-rules argument to the session state.
 * Returns "usage" for an unrecognized argument; "" and "status" only report.
 */
export function applyCommand(state: ExtensionState, arg: string): "on" | "off" | "status" | "usage" {
	switch (arg.trim().toLowerCase()) {
		case "on":
			state.enabled = true;
			return "on";
		case "off":
			state.enabled = false;
			return "off";
		case "":
		case "status":
			return "status";
		default:
			return "usage";
	}
}

export function statusText(
	state: ExtensionState,
	env: NodeJS.ProcessEnv,
	resolved: ResolvedRulesRoot | null,
): string {
	const flags = [state.enabled ? "on" : "off"];
	if (envDisabled(env)) {
		flags.push("PI_KANDER_RULES=0");
	}
	return `kander-rules ${flags.join(" ")} · ${resolved ? resolved.root : "no rules root found"}`;
}

export default function kanderRulesExtension(pi: ExtensionAPI) {
	const state: ExtensionState = { enabled: true };
	let cachedCwd = "";
	let cachedRoot: ResolvedRulesRoot | null = null;

	const resolveFor = (cwd: string): ResolvedRulesRoot | null => {
		// Re-resolve when the cwd moved or the cached root lost its entry file;
		// a rules install or removal mid-session is picked up on the next turn.
		if (cachedCwd !== cwd || (cachedRoot !== null && !fs.existsSync(path.join(cachedRoot.root, ENTRY_FILE)))) {
			cachedCwd = cwd;
			cachedRoot = resolveRulesRoot(cwd);
		}
		return cachedRoot;
	};

	pi.on("session_start", async (_event, ctx) => {
		const resolved = resolveFor(ctx.cwd ?? process.cwd());
		if (resolved && state.enabled && !envDisabled(process.env)) {
			ctx.ui.notify(`Kander rules: ${resolved.root}`, "info");
		}
	});

	pi.on("before_agent_start", async (event, ctx) => {
		if (!shouldInject(state, process.env, event.systemPromptOptions?.contextFiles)) {
			return;
		}
		const resolved = resolveFor(ctx.cwd ?? process.cwd());
		if (!resolved) {
			return;
		}
		const block = buildInjection(resolved.root);
		if (!block) {
			return;
		}
		return { systemPrompt: event.systemPrompt + "\n\n" + block };
	});

	pi.registerCommand("kander-rules", {
		description: "Kander rules injection: on | off | status",
		handler: async (args, ctx) => {
			const result = applyCommand(state, args ?? "");
			if (result === "usage") {
				ctx.ui.notify("Usage: /kander-rules on|off|status", "warning");
				return;
			}
			const resolved = resolveFor(ctx.cwd ?? process.cwd());
			ctx.ui.notify(statusText(state, process.env, resolved), "info");
		},
	});
}
