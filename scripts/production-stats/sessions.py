#!/usr/bin/env python3
"""Resolve the executor model of each card from the agents' own session logs.

Usage:
    python3 -I sessions.py --board board.json --out models.json [--exclude-session ID]...

Cards carry no executor model. This script finds the executing session in
three ways, in order:

1. the card's `SESSION` field (`<agent> <session-id>`);
2. for Codex cards without a session ID, the rollout whose first user
   message names the card;
3. a `kander pick <task-id>` inside an interactive Claude Code or Codex session;
4. a Claude Code sub-agent whose log names the card first.

The model is the one the session used most. Requires `rg` (ripgrep) and, for
compressed Codex rollouts, `zstd`. Pass `--exclude-session` with the ID of any
session that only discussed card IDs, such as the session running this script.
"""
import argparse
import collections
import glob
import json
import os
import re
import sqlite3
import subprocess

HOME = os.path.expanduser("~")
CLAUDE = os.path.join(HOME, ".claude", "projects")
CODEX = os.path.join(HOME, ".codex", "sessions")
CODEX_ARCHIVE = os.path.join(HOME, ".codex", "archived_sessions")
MODEL = re.compile(r'"model":"([^"]+)"')
PICK = re.compile(r"kander\S*\s+pick\s+(\d{8}-[a-z0-9-]+)")
UUID = re.compile(r"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl")


def mode(counter):
    counter.pop("<synthetic>", None)
    return counter.most_common(1)[0][0] if counter else None


def read_text(path):
    if path.endswith(".zst"):
        out = subprocess.run(["zstd", "-dc", path], capture_output=True).stdout
        return out.decode("utf-8", "replace")
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def rg_index(args):
    """Run rg and return (path, task-id) pairs from `path:task-id` lines."""
    out = subprocess.run(["rg"] + args, capture_output=True, text=True).stdout
    pairs = []
    for line in out.splitlines():
        path, _, task_id = line.rpartition(":")
        if path and task_id:
            pairs.append((path, task_id))
    return pairs


def excluded(path, sessions):
    return any(s in path for s in sessions)


def claude_models(lines, main_only):
    c = collections.Counter()
    for line in lines:
        if '"type":"assistant"' in line and not (main_only and '"isSidechain":true' in line):
            m = MODEL.search(line)
            if m:
                c[m.group(1)] += 1
    return mode(c)


def codex_models(lines):
    c, effort = collections.Counter(), collections.Counter()
    for line in lines:
        if '"turn_context"' not in line:
            continue
        try:
            payload = json.loads(line).get("payload") or {}
        except ValueError:
            continue
        if payload.get("model"):
            c[payload["model"]] += 1
        e = payload.get("effort") or payload.get("reasoning_effort")
        if e:
            effort[e] += 1
    m = mode(c)
    return (m, effort.most_common(1)[0][0] if effort else "") if m else None


class Resolver:
    def __init__(self, exclude, cards):
        self.claude = {os.path.basename(p)[:-6]: p for p in glob.glob(os.path.join(CLAUDE, "*", "*.jsonl"))}
        self.codex = {}
        rollouts = []
        for root in (CODEX, CODEX_ARCHIVE):
            rollouts += glob.glob(os.path.join(root, "**", "rollout-*.jsonl*"), recursive=True)
        for p in rollouts:
            m = UUID.search(p)
            if m:
                self.codex[m.group(1)] = p
        self.codex_tasks = self.index_codex_tasks(rollouts, cards)
        pick = r"kander\S*\s+pick\s+([0-9]{8}-[a-z0-9-]+)"
        self.picks = {}
        for agent, args in (
            ("claude", ["-o", "--with-filename", pick, "-r", "$1", CLAUDE, "-g", "*.jsonl", "-g", "!**/subagents/**"]),
            ("codex", ["-z", "-o", "--with-filename", pick, "-r", "$1", CODEX]),
        ):
            for path, task_id in sorted(rg_index(args)):
                if not excluded(path, exclude):
                    self.picks.setdefault((agent, task_id), path)
        # The first task ID in a sub-agent log names its card; the largest log wins.
        self.subagents = {}
        for path, task_id in rg_index(["-m1", "-o", "--with-filename", r"20[0-9]{6}-[a-z0-9-]+?-task", CLAUDE, "-g", "**/subagents/*.jsonl"]):
            if excluded(path, exclude):
                continue
            old = self.subagents.get(task_id)
            if old is None or os.path.getsize(path) > os.path.getsize(old):
                self.subagents[task_id] = path

    @staticmethod
    def index_codex_tasks(rollouts, cards):
        """Map Codex cards without a session ID to the rollout whose first user turn names them."""
        wanted = [t for t, c in cards if c["owner"] == "codex" and not (c["session_agent"] == "codex" and c["session_id"])]
        if not wanted:
            return {}
        pattern = re.compile("|".join(re.escape(t) for t in sorted(wanted, key=len, reverse=True)))
        found = {}
        for path in rollouts:
            if path.endswith(".zst"):
                with subprocess.Popen(["zstd", "-dc", path], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as proc:
                    head = proc.stdout.read(200000).decode("utf-8", "replace")
                    proc.kill()
            else:
                with open(path, encoding="utf-8", errors="replace") as f:
                    head = f.read(200000)
            for line in head.splitlines():
                if '"type":"user_message"' in line or ('"role":"user"' in line and "kander" in line.lower()):
                    m = pattern.search(line)
                    if m:
                        found.setdefault(m.group(0), path)
                        break
        return found

    def by_session(self, agent, sid):
        if agent == "claude" and sid in self.claude:
            return claude_models(read_text(self.claude[sid]).splitlines(), True)
        if agent == "codex" and sid in self.codex:
            return codex_models(read_text(self.codex[sid]).splitlines())
        if agent == "grok":
            for d in glob.glob(os.path.join(HOME, ".grok", "sessions", "*", sid)):
                for name in ("chat_history.jsonl", "updates.jsonl"):
                    p = os.path.join(d, name)
                    if os.path.exists(p):
                        found = mode(collections.Counter(re.findall(r'"model_?[iI]d":"([^"]+)"', read_text(p))))
                        if found:
                            return found
        if agent == "cursor":
            for db in glob.glob(os.path.join(HOME, ".cursor", "chats", "*", sid, "store.db")):
                try:
                    con = sqlite3.connect("file:" + db + "?mode=ro", uri=True)
                    rows = con.execute("select value from meta").fetchall()
                    con.close()
                except sqlite3.Error:
                    continue
                for (value,) in rows:
                    try:
                        meta = json.loads(bytes.fromhex(value if isinstance(value, str) else value.decode()).decode())
                    except (ValueError, UnicodeDecodeError):
                        continue
                    if meta.get("lastUsedModel"):
                        return meta["lastUsedModel"]
        if agent == "pi":
            for p in glob.glob(os.path.join(HOME, ".pi", "agent", "sessions", "*", "*_" + sid + ".jsonl")):
                return mode(collections.Counter(MODEL.findall(read_text(p))))
        if agent == "devin":
            p = os.path.join(HOME, ".local", "share", "devin", "cli", "transcripts", sid + ".json")
            if os.path.exists(p):
                return mode(collections.Counter(re.findall(r'"model_name": ?"([^"]+)"', read_text(p))))
        return None

    def by_pick(self, agent, task_id):
        path = self.picks.get((agent, task_id))
        if not path:
            return None
        text = read_text(path)
        start = next((m.start() for m in PICK.finditer(text) if m.group(1) == task_id), None)
        if start is None:
            return None
        lines = text[start:].splitlines()[:3000]
        return claude_models(lines, False) if agent == "claude" else codex_models(lines)

    def by_subagent(self, task_id):
        path = self.subagents.get(task_id)
        return claude_models(read_text(path).splitlines(), False) if path else None

    def resolve(self, card, task_id):
        owner, agent, sid = card["owner"], card["session_agent"], card["session_id"]
        attempts = [("session", lambda: self.by_session(agent, sid) if sid else None)]
        if owner == "codex" and task_id in self.codex_tasks:
            attempts.append(("session", lambda: codex_models(read_text(self.codex_tasks[task_id]).splitlines())))
        if owner in ("claude", "codex"):
            attempts.append(("pick", lambda: self.by_pick(owner, task_id)))
        if owner == "claude":
            attempts.append(("subagent", lambda: self.by_subagent(task_id)))
        for source, attempt in attempts:
            found = attempt()
            if found:
                model, effort = found if isinstance(found, tuple) else (found, "")
                return {"agent": owner, "model": model, "effort": effort, "source": source}
        return {"agent": owner, "model": "", "effort": "", "source": ""}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--board", required=True, help="output of collect.py")
    parser.add_argument("--out", required=True)
    parser.add_argument("--exclude-session", action="append", default=[], metavar="ID")
    args = parser.parse_args()
    with open(args.board, encoding="utf-8") as f:
        board = json.load(f)
    cards = [(t, c) for data in board["projects"].values() for t, c in data["cards"].items()]
    resolver = Resolver(args.exclude_session, cards)
    out, sources = {}, collections.Counter()
    for project, data in board["projects"].items():
        for task_id, card in data["cards"].items():
            r = resolver.resolve(card, task_id)
            out[project + "/" + task_id] = r
            sources[(r["agent"], r["source"] or "unresolved")] += 1
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False)
    for (agent, source), n in sorted(sources.items()):
        print("%s %s: %d" % (agent, source, n))


if __name__ == "__main__":
    main()
