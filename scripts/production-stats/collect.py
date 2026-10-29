#!/usr/bin/env python3
"""Collect read-only facts from the done cards of one or more Kander boards.

Usage:
    python3 -I collect.py --out board.json [--cutoff YYYYMMDD] NAME=PROJECT_ROOT...

Each PROJECT_ROOT must contain `kanban/done/`. The output JSON holds, per
project, the cards, review runs (deduplicated by run ID), review batches
(deduplicated by batch ID), and the latest disposition record of each finding.
"""
import argparse
import datetime
import glob
import json
import os
import re
import sys

# Card header keys, including the Chinese keys of older cards.
KEYS = {
    "OWNER": "owner", "负责人": "owner",
    "SESSION": "session", "会话": "session",
    "STARTED_AT": "started", "开始时间": "started",
    "FINISHED_AT": "finished", "完成时间": "finished",
    "SIZE": "size", "TYPE": "type", "类型": "type",
}
AGENTS = ["claude", "codex", "grok", "cursor", "pi", "devin", "opencode", "kimi"]
HEADER = re.compile(r"^- ([^:\s][^:]*?):\s*(.*)$")
FINDINGS = re.compile(r"```kander-findings\s*\n(.*?)\n```", re.S)
TIME = re.compile(r"(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2})")


def parse_header(path):
    header = {}
    with open(path, encoding="utf-8", errors="replace") as f:
        for i, line in enumerate(f):
            if i > 60 or (line.startswith("## ") and header):
                break
            m = HEADER.match(line.rstrip("\n"))
            if m and m.group(1) in KEYS and KEYS[m.group(1)] not in header:
                header[KEYS[m.group(1)]] = m.group(2).strip()
    return header


def normalize_owner(value):
    value = (value or "").lower()
    for agent in AGENTS:
        if re.search(r"\b" + agent + r"\b", value):
            return agent
    return value or "unknown"


def parse_time(value):
    m = TIME.match(value or "")
    if not m:
        return None
    return datetime.datetime.strptime(m.group(1) + " " + m.group(2), "%Y-%m-%d %H:%M")


def load_json(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def parse_findings(report):
    """Return the must-fix and non-blocking IDs of the report's kander-findings block."""
    try:
        with open(report, encoding="utf-8", errors="replace") as f:
            blocks = FINDINGS.findall(f.read())
    except OSError:
        return None
    if not blocks:
        return None
    try:
        data = json.loads(blocks[-1])
    except ValueError:
        return None
    must = data.get("FINDINGS") or []
    nonblocking = data.get("NON_BLOCKING") or []
    return {
        "must": [x.get("id") for x in must],
        "nonblocking": [x.get("id") for x in nonblocking],
        "tiers": {x.get("id"): x.get("tier") for x in must + nonblocking},
    }


def collect_card_reviews(path, runs, batches, dispositions):
    reviews = os.path.join(path, "reviews")
    for sidecar in glob.glob(os.path.join(reviews, "*", "sidecar.json")):
        d = load_json(sidecar)
        if not d or not d.get("run_id"):
            continue
        run_id = d["run_id"]
        run_dir = os.path.dirname(sidecar)
        if run_id not in runs:
            assignment = load_json(os.path.join(run_dir, "assignment.json")) or {}
            runs[run_id] = {
                "batch": d.get("batch_id"), "role": d.get("role"), "reviewer": d.get("reviewer"),
                "model": d.get("model") or "", "effort": d.get("effort") or "",
                "commit": d.get("commit"), "task_ids": d.get("task_ids") or [],
                "execution_status": d.get("execution_status"), "created": d.get("created_at"),
                "findings": parse_findings(os.path.join(run_dir, "report.md")),
                "items": assignment.get("items") or {},
            }
        for record_path in glob.glob(os.path.join(run_dir, "dispositions", "*.json")):
            r = load_json(record_path)
            if not r:
                continue
            key = "|".join([r.get("run_id") or run_id, r.get("finding_id") or "", r.get("task_id") or ""])
            old = dispositions.get(key)
            if old is None or (r.get("recorded_at") or "") > (old.get("recorded_at") or ""):
                dispositions[key] = {k: r.get(k) for k in ("run_id", "finding_id", "task_id", "status", "recorded_at")}
    for path in glob.glob(os.path.join(reviews, "batches", "*", "disposition.json")):
        d = load_json(path)
        if d is None:
            continue
        batch = d.get("batch") or {}
        batch_id = batch.get("batch_id") or os.path.basename(os.path.dirname(path))
        batches.setdefault(batch_id, {"task_ids": batch.get("task_ids") or []})


def collect_project(root, cutoff):
    done = os.path.join(root, "kanban", "done")
    cards, runs, batches, dispositions = {}, {}, {}, {}
    for name in sorted(os.listdir(done)):
        if not re.match(r"\d{8}", name) or name[:8] > cutoff:
            continue
        path = os.path.join(done, name)
        spec = os.path.join(path, "spec.md") if os.path.isdir(path) else path
        if not os.path.isfile(spec):
            continue
        task_id = name[:-3] if name.endswith(".md") else name
        h = parse_header(spec)
        started, finished = parse_time(h.get("started")), parse_time(h.get("finished"))
        session = (h.get("session") or "").split()
        cards[task_id] = {
            "date": name[:8], "owner": normalize_owner(h.get("owner")),
            "size": (h.get("size") or "").lower(),
            "session_agent": session[0] if session else "",
            "session_id": session[1] if len(session) > 1 else "",
            "has_plan": os.path.isfile(os.path.join(path, "reviews", "plan.json")),
            "minutes": (finished - started).total_seconds() / 60 if started and finished and finished >= started else None,
        }
        if os.path.isdir(path):
            collect_card_reviews(path, runs, batches, dispositions)
    return {"cards": cards, "runs": runs, "batches": batches, "dispositions": list(dispositions.values())}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", required=True)
    parser.add_argument("--cutoff", default="99999999", help="last card ID date to include, YYYYMMDD")
    parser.add_argument("projects", nargs="+", metavar="NAME=PROJECT_ROOT")
    args = parser.parse_args()
    data = {"cutoff": args.cutoff, "projects": {}}
    for spec in args.projects:
        name, sep, root = spec.partition("=")
        if not sep or not os.path.isdir(os.path.join(root, "kanban", "done")):
            sys.exit("not a NAME=PROJECT_ROOT with kanban/done/: " + spec)
        data["projects"][name] = collect_project(os.path.expanduser(root), args.cutoff)
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False)
    for name, p in data["projects"].items():
        print("%s: %d cards, %d runs, %d batches, %d dispositions" % (
            name, len(p["cards"]), len(p["runs"]), len(p["batches"]), len(p["dispositions"])))


if __name__ == "__main__":
    main()
