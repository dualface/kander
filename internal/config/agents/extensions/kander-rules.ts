/**
 * Kander Rules Extension
 * kander-extension-version: 2
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
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export const KANDER_RULES_EXTENSION_VERSION = "2";
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
}

export interface ResolvedRulesRoot {
	root: string;
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

/**
 * Resolve the Kander rules root: the global rules root (~/.agents/kander/),
 * which counts only when the entry file exists. Kander installs globally
 * only, so a .kander/rules/ directory a retired project install left in a
 * checkout is never read.
 */
export function resolveRulesRoot(deps: RootDeps = {}): ResolvedRulesRoot | null {
	const exists = deps.exists ?? ((target: string) => fs.existsSync(target));
	const home = (deps.homedir ?? os.homedir)();
	const globalRules = path.join(home, ".agents", "kander");
	if (exists(path.join(globalRules, ENTRY_FILE))) {
		return { root: globalRules };
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

	pi.on("before_agent_start", async (event, ctx) => {
		if (!shouldInject(state, process.env, event.systemPromptOptions?.contextFiles)) {
			return;
		}
		// Resolve every turn: an install that appears mid-session takes effect
		// without restarting pi.
		const resolved = resolveRulesRoot();
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
			const resolved = resolveRulesRoot();
			ctx.ui.notify(statusText(state, process.env, resolved), "info");
		},
	});
}
