#!/usr/bin/env python3
"""Print the numbers and Markdown tables used in docs/production-stats*.md.

Usage:
    python3 -I report.py --board board.json [--models models.json]

`board.json` comes from collect.py and `models.json` from sessions.py. Without
`--models`, every executor model is reported as not resolved.
"""
import argparse
import collections
import json
import statistics

MUST = ("blocking", "high", "medium")
ROLE_GROUP = {"PMQA": "PMQA", "Security": "Security", "PM": "PM / QA", "QA": "PM / QA"}
AGENT_NAMES = {"claude": "Claude Code", "codex": "Codex", "grok": "Grok", "cursor": "Cursor",
               "pi": "Pi", "devin": "Devin", "opencode": "OpenCode", "kimi": "Kimi", "droid": "Droid"}
MIN_EXEC_CARDS = 10
MIN_REVIEW_RUNS = 20


def pct(a, b, digits=0):
    return ("%." + str(digits) + "f%%") % (100.0 * a / b) if b else "-"


def num(n):
    return "{:,}".format(n)


def agent_name(a):
    return AGENT_NAMES.get(a, a)


def table_row(cells):
    return ("| " + " | ".join(cells) + " |").replace("|  |", "| |").replace("|  |", "| |")


def assigned(run, task_id):
    """Must-fix findings of the run assigned to the card."""
    f = run["findings"]
    if not f or not f["must"]:
        return 0
    if run["items"]:
        return sum(1 for fid in f["must"] if task_id in (run["items"].get(fid) or []))
    return len(f["must"])


def projects_section(projects):
    print("## Projects\n")
    print("| Project | Period | Cards done | Review batches | Batches with must-fix issues | Review runs | Must-fix issues |")
    print("| --- | --- | ---: | ---: | ---: | ---: | ---: |")
    total = collections.Counter()
    dates, minutes = [], []
    for name, p in projects.items():
        days = sorted(c["date"] for c in p["cards"].values())
        dates += days
        minutes += [c["minutes"] for c in p["cards"].values() if c["minutes"] is not None]
        hit_batches = {r["batch"] for r in p["runs"].values() if r["findings"] and r["findings"]["must"]}
        n_hit = len([b for b in p["batches"] if b in hit_batches])
        must = sum(len(r["findings"]["must"]) for r in p["runs"].values() if r["findings"])
        row = {"cards": len(p["cards"]), "batches": len(p["batches"]), "hit": n_hit, "runs": len(p["runs"]), "must": must,
               "planned": sum(1 for c in p["cards"].values() if c["has_plan"]),
               "reviewed": len({t for r in p["runs"].values() for t in r["task_ids"]} & set(p["cards"]))}
        total.update(row)
        print("| %s | %s-%s to %s-%s | %s | %s | %s (%s) | %s | %s |" % (
            name, days[0][4:6], days[0][6:], days[-1][4:6], days[-1][6:], num(row["cards"]), num(row["batches"]),
            num(n_hit), pct(n_hit, row["batches"]), num(row["runs"]), num(must)))
    print("| **Total** | %s to %s | **%s** | **%s** | **%s (%s)** | **%s** | **%s** |\n" % (
        min(dates), max(dates), num(total["cards"]), num(total["batches"]), num(total["hit"]),
        pct(total["hit"], total["batches"]), num(total["runs"]), num(total["must"])))
    print("- Cards with structured review records: %s; with at least one review run: %s" % (num(total["planned"]), num(total["reviewed"])))
    print("- Cards with start and finish times: %s; median minutes: %s\n" % (num(len(minutes)), statistics.median(minutes)))


def agents_section(projects):
    owners, reviewers, tiers, statuses = collections.Counter(), collections.Counter(), collections.Counter(), collections.Counter()
    cross = [0, 0]
    for p in projects.values():
        for c in p["cards"].values():
            owners[c["owner"]] += 1
        for r in p["runs"].values():
            reviewers[r["reviewer"]] += 1
            card_owners = {p["cards"][t]["owner"] for t in r["task_ids"] if t in p["cards"]}
            if card_owners:
                cross[1] += 1
                cross[0] += r["reviewer"] not in card_owners
            if r["findings"]:
                for fid in r["findings"]["must"]:
                    tiers[r["findings"]["tiers"].get(fid)] += 1
        for d in p["dispositions"]:
            statuses[d["status"]] += 1
    print("## Agents\n")
    print("| Agent | Cards executed | Review runs |")
    print("| --- | ---: | ---: |")
    for a, n in owners.most_common():
        print("| %s | %s | %s |" % (agent_name(a), num(n), num(reviewers.get(a, 0))))
    disposed = sum(statuses.values())
    print("\n- Reviews by a different agent from the executor: %s of %s (%s)" % (num(cross[0]), num(cross[1]), pct(*cross)))
    print("- Must-fix tiers: %s; medium share %s" % (dict(tiers), pct(tiers.get("medium", 0), sum(tiers.values()))))
    print("- Final dispositions: %s %s; fixed %s, rejected %s\n" % (
        num(disposed), dict(statuses), pct(statuses["fixed"], disposed), pct(statuses["rejected"], disposed, 1)))


def executor_section(projects, models):
    # A review round is one (batch, reviewed commit); it sends the card back when
    # any normally finished run in it has a must-fix finding assigned to the card.
    stats = collections.defaultdict(collections.Counter)
    for name, p in projects.items():
        rounds = collections.defaultdict(lambda: collections.defaultdict(int))
        for r in p["runs"].values():
            for t in r["task_ids"]:
                if t in p["cards"]:
                    rounds[t][(r["batch"], r["commit"])] += assigned(r, t)
        for t, rs in rounds.items():
            card = p["cards"][t]
            model = (models.get(name + "/" + t) or {}).get("model") or ""
            s = stats[(card["owner"], model)]
            sendbacks = sum(1 for n in rs.values() if n > 0)
            s["cards"] += 1
            s["large"] += card["size"] == "large"
            s["rounds"] += len(rs)
            s["sendbacks"] += sendbacks
            s["sent_back"] += sendbacks > 0
            s["must"] += sum(rs.values())
    print("## Executor send-backs\n")
    print("| Executor | Model | Reviewed cards | Large cards | Review rounds | Send-backs | Send-backs per card | Cards sent back at least once | Must-fix issues per card |")
    print("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
    total, small = collections.Counter(), collections.Counter()

    def row(label, model, s, bold=False):
        values = [num(s["cards"]), pct(s["large"], s["cards"]), num(s["rounds"]), num(s["sendbacks"]),
                  "%.2f" % (s["sendbacks"] / s["cards"]), "%s (%s)" % (num(s["sent_back"]), pct(s["sent_back"], s["cards"])),
                  "%.2f" % (s["must"] / s["cards"])]
        if bold:
            values = ["**%s**" % v for v in values]
        print(table_row([label, model] + values))
    for (agent, model), s in sorted(stats.items(), key=lambda kv: -kv[1]["cards"]):
        total.update(s)
        if s["cards"] < MIN_EXEC_CARDS:
            small.update(s)
            continue
        row(agent_name(agent), "`%s`" % model if model else "not resolved", s)
    row("**All** (including %s cards in smaller rows)" % num(small["cards"]), "", total, True)
    resolved = sum(s["cards"] for (a, m), s in stats.items() if m)
    print("\n- Executor model resolved for %s of %s reviewed cards (%s)" % (num(resolved), num(total["cards"]), pct(resolved, total["cards"])))
    print("- Review rounds per card: %.2f\n" % (total["rounds"] / total["cards"]))


def reviewer_section(projects):
    stats = collections.defaultdict(collections.Counter)
    for name, p in projects.items():
        keys = {}
        for run_id, r in p["runs"].items():
            k = (ROLE_GROUP.get(r["role"], "other"), r["reviewer"], r["model"] or "(default)")
            keys[run_id] = k
            s = stats[k]
            s["runs"] += 1
            if r["findings"] is not None:
                s["parsed"] += 1
                s["hit"] += bool(r["findings"]["must"])
                s["must"] += len(r["findings"]["must"])
        for d in p["dispositions"]:
            k = keys.get(d["run_id"])
            if not k:
                continue
            f = p["runs"][d["run_id"]]["findings"]
            tier = f["tiers"].get(d["finding_id"]) if f else None
            s = stats[k]
            s["disp"] += 1
            s["rejected"] += d["status"] == "rejected"
            s["deferred"] += d["status"] == "deferred"
            if tier in MUST:
                s["must_disp"] += 1
                s["must_rejected"] += d["status"] == "rejected"
            else:
                s["nb_disp"] += 1
    print("## Reviewer findings, rejections, and deferrals\n")
    print("| Role | Reviewer | Model | Review runs | Runs with must-fix issues | Must-fix issues per run | Dispositions | Rejected | Must-fix rejected | Deferred |")
    print("| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")

    def row(cells, s, bold=False):
        values = [num(s["runs"]), "%s (%s)" % (num(s["hit"]), pct(s["hit"], s["parsed"])),
                  "%.2f" % (s["must"] / s["parsed"] if s["parsed"] else 0), num(s["disp"]),
                  "%s (%s)" % (num(s["rejected"]), pct(s["rejected"], s["disp"], 1)),
                  "%s / %s (%s)" % (num(s["must_rejected"]), num(s["must_disp"]), pct(s["must_rejected"], s["must_disp"], 1)),
                  "%s (%s)" % (num(s["deferred"]), pct(s["deferred"], s["disp"], 1))]
        if bold:
            values = ["**%s**" % v for v in values]
        print(table_row(cells + values))
    everything = collections.Counter()
    for group in ("PMQA", "PM / QA", "Security"):
        rows = sorted(((k, s) for k, s in stats.items() if k[0] == group), key=lambda kv: -kv[1]["runs"])
        total = collections.Counter()
        for (g, reviewer, model), s in rows:
            total.update(s)
            if s["runs"] >= MIN_REVIEW_RUNS:
                row([g, agent_name(reviewer), "`%s`" % model], s)
        row(["**%s, all**" % group, "", ""], total, True)
    for s in stats.values():
        everything.update(s)
    row(["**All roles**", "", ""], everything, True)
    other = sum(s["runs"] for k, s in stats.items() if k[0] == "other")
    print("\n- Runs that finished normally: %s; runs of other roles: %s" % (num(everything["parsed"]), num(other)))
    print("- Non-blocking dispositions: %s; deferred %s (%s)\n" % (
        num(everything["nb_disp"]), num(everything["deferred"]), pct(everything["deferred"], everything["nb_disp"], 1)))


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--board", required=True, help="output of collect.py")
    parser.add_argument("--models", help="output of sessions.py")
    args = parser.parse_args()
    with open(args.board, encoding="utf-8") as f:
        board = json.load(f)
    models = {}
    if args.models:
        with open(args.models, encoding="utf-8") as f:
            models = json.load(f)
    projects = board["projects"]
    # Findings count only for runs that finished normally.
    for p in projects.values():
        for r in p["runs"].values():
            if r["execution_status"] != "ok":
                r["findings"] = None
    print("# Production stats (cutoff %s)\n" % board["cutoff"])
    projects_section(projects)
    agents_section(projects)
    executor_section(projects, models)
    reviewer_section(projects)


if __name__ == "__main__":
    main()
