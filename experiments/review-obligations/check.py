"""Run the offline experiment; put logs and vectors outside the checkout."""

import argparse
import collections
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import time
import urllib.request

import generate

ROOT = pathlib.Path(__file__).resolve().parent
JAR_VERSION = "1.8.0"
JAR_SHA256 = "e1f11b75c4b5bc4478ba0ac5cb6ae28e40a01b0016bc33e0aa97da22dfdbcd7f"
JAR_URL = f"https://github.com/tlaplus/tlaplus/releases/download/v{JAR_VERSION}/tla2tools.jar"


def tool_jar(path, fetch):
    if not path.exists():
        if not fetch:
            raise RuntimeError(f"missing {path}; pass --fetch-tools or --jar PATH")
        path.parent.mkdir(parents=True, exist_ok=True)
        with urllib.request.urlopen(JAR_URL, timeout=60) as response:
            data = response.read()
        if hashlib.sha256(data).hexdigest() != JAR_SHA256:
            raise RuntimeError("downloaded TLC checksum mismatch")
        path.write_bytes(data)
    if hashlib.sha256(path.read_bytes()).hexdigest() != JAR_SHA256:
        raise RuntimeError("TLC checksum mismatch")
    return path.resolve()


def evaluate(expr, state, constants):
    """Reference interpreter: no generated Go or TLA expressions are evaluated."""
    if type(expr) is int:
        return expr
    if isinstance(expr, str):
        return state[expr] if expr in state else constants[expr]
    op, *args = expr
    if op == "if":
        return evaluate(args[1] if evaluate(args[0], state, constants) else args[2], state, constants)
    values = [evaluate(arg, state, constants) for arg in args]
    if op == "and":
        return all(values)
    if op == "or":
        return any(values)
    a, b = values
    return {"=": lambda: a == b, "!=": lambda: a != b, ">": lambda: a > b,
            "<": lambda: a < b, "+": lambda: a + b}[op]()


def vector(case, before, after, member, event, bounds, accepted=True):
    return dict(case=case, before=before, after=after, member=member,
                event=event, bounds=bounds, accepted=accepted)


def write_vector(stream, item):
    stream.write(json.dumps(item, separators=(",", ":")) + "\n")


def reference_vectors(spec, stream):
    """Enumerate every event, including refusals, at every reachable local state."""
    bounds = dict(MaxCycle=3, MaxBatch=3, Mutation=0)
    fields = list(spec["initial"])
    initial = tuple(spec["initial"].values())
    seen, queue = {initial}, collections.deque([initial])
    accepted_count, rejected_count = 0, 0
    while queue:
        key = queue.popleft()
        state = dict(zip(fields, key))
        for event, action in spec["actions"].items():
            accepted = evaluate(action["guard"], state, bounds)
            after = state.copy()
            if accepted:
                after.update({k: evaluate(v, state, bounds) for k, v in action["set"].items()})
            write_vector(stream, vector("reference-exhaustive", [state], [after], 0, event, bounds, accepted))
            if accepted:
                accepted_count += 1
                next_key = tuple(after[f] for f in fields)
                if next_key not in seen:
                    seen.add(next_key)
                    queue.append(next_key)
            else:
                rejected_count += 1
    return dict(states=len(seen), accepted=accepted_count, refused=rejected_count)


def trace_vectors(path, name, bounds):
    raw = json.loads(path.read_text())
    transitions = raw["counterexample"]["action"]
    vectors, labels = [], []
    for before, action, after in transitions:
        context = action["context"]
        member, event = context["m"] - 1, context["event"]
        if before[1]["state"][member]["done"]:
            raise RuntimeError("trace contains a transition after completion")
        vectors.append(vector(name, before[1]["state"], after[1]["state"], member, event, bounds))
        labels.append(f"{member + 1}:{event}")
    if not vectors:
        raise RuntimeError("expected a nonempty TLC trace")
    return vectors, labels, transitions[-1][2][1]


def model_check(jar, root, name, members, cycles, batches, mutation=0, witness=None):
    path = root / name
    path.mkdir()
    for source in ROOT.glob("*.tla"):
        shutil.copyfile(source, path / source.name)
    invariants = "DebtSafety BoundSafety MemberIsolation TypeOK"
    if witness:
        invariants += " " + witness
    (path / "Model.cfg").write_text(
        f"CONSTANTS MemberCount = {members} MaxCycle = {cycles} MaxBatch = {batches} Mutation = {mutation}\n"
        f"SPECIFICATION Spec\nINVARIANTS {invariants}\nCHECK_DEADLOCK FALSE\n")
    command = ["java", "-XX:+UseParallelGC", "-Xmx1g", "-cp", str(jar), "tlc2.TLC",
               "-workers", "1", "-fp", "0", "-seed", "1", "-noGenerateSpecTE",
               "-dumpTrace", "json", "trace.json", "Model"]
    start = time.monotonic()
    result = subprocess.run(command, cwd=path, text=True, capture_output=True, timeout=120)
    output = result.stdout + result.stderr
    (path / "output.log").write_text(output)
    expected_violation = witness or ("DebtSafety" if mutation else None)
    if expected_violation:
        if result.returncode != 12 or f"Invariant {expected_violation} is violated" not in output:
            raise RuntimeError(f"{name}: expected {expected_violation} counterexample, see {path}")
    elif result.returncode or "Model checking completed. No error has been found." not in output:
        raise RuntimeError(f"{name}: incomplete or failed model check, see {path}")
    stats = re.search(r"([\d,]+) states generated, ([\d,]+) distinct states found, ([\d,]+) states left on queue", output)
    if not stats:
        raise RuntimeError(f"{name}: missing search statistics")
    generated, distinct, remaining = (int(s.replace(",", "")) for s in stats.groups())
    if not expected_violation and remaining:
        raise RuntimeError(f"{name}: incomplete search")
    bounds = dict(MaxCycle=cycles, MaxBatch=batches, Mutation=mutation)
    row = dict(name=name, members=members, bounds=bounds, generated=generated,
               distinct=distinct, remaining=remaining, seconds=round(time.monotonic() - start, 3),
               result="witness" if witness else "detected" if mutation else "complete")
    vectors = []
    if expected_violation:
        vectors, labels, final = trace_vectors(path / "trace.json", name, bounds)
        if witness and (not all(s["done"] for s in final["state"]) or any(o["debts"] for o in final["observer"])):
            raise RuntimeError(f"{name}: witness does not complete without debt")
        row["trace"] = labels
        (path / "trace.txt").write_text(" -> ".join(labels) + "\n")
    print(json.dumps(row), flush=True)
    return row, vectors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--jar", type=pathlib.Path, default=pathlib.Path.home() / ".cache/kander-formal/tla2tools-1.8.0.jar")
    parser.add_argument("--fetch-tools", action="store_true")
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    jar = tool_jar(args.jar, args.fetch_tools)
    output = args.output.resolve() if args.output else pathlib.Path(tempfile.mkdtemp(prefix="kander-obligations-"))
    if args.output:
        output.mkdir(parents=True, exist_ok=False)
    print(f"Artifacts: {output}", flush=True)
    subprocess.run(["python3", str(ROOT / "generate.py"), "--check"], check=True)
    subprocess.run(["python3", "-m", "unittest", "-v", "test_generator"], cwd=ROOT, check=True)
    summary = dict(tlc_version=JAR_VERSION, tlc_sha256=JAR_SHA256, checks=[],
                   source_sha256={name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest()
                                  for name in ["transitions.json", "generate.py", "Generated.tla",
                                               "generated.go", "Oracle.tla", "Model.tla", "check.py"]})
    cases = [
        ("single", 1, 3, 3, 0, None),
        ("pair", 2, 2, 2, 0, None),
        ("reset-lost", 1, 3, 3, 1, None),
        ("old-closure", 1, 3, 3, 2, None),
        ("release-crosses-kind", 1, 3, 3, 3, None),
        ("release-forgets-history", 1, 3, 3, 4, None),
        ("completion", 2, 2, 2, 0, "NoCompletion"),
        ("twice-reset-completion", 1, 3, 3, 0, "NoReviewedRecoveryCompletion"),
        ("released-role-completion", 1, 3, 3, 0, "NoReleasedRoleCompletion"),
    ]
    with (output / "tlc-vectors.jsonl").open("w") as trace_stream, (output / "vectors.jsonl").open("w") as all_stream:
        for case in cases:
            row, vectors = model_check(jar, output, *case)
            summary["checks"].append(row)
            for item in vectors:
                write_vector(trace_stream, item)
                write_vector(all_stream, item)
        summary["reference"] = reference_vectors(generate.load(ROOT / "transitions.json"), all_stream)
    env = dict(os.environ, KANDER_OBLIGATION_VECTORS=str(output / "vectors.jsonl"))
    result = subprocess.run(["go", "test", "-count=1", "-v", "."], cwd=ROOT, env=env,
                            text=True, capture_output=True, timeout=120)
    (output / "go-test.log").write_text(result.stdout + result.stderr)
    print(result.stdout, flush=True)
    if result.returncode:
        raise RuntimeError(f"Go trace comparison failed: {output / 'go-test.log'}")
    summary["go_comparison"] = "pass"
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(f"Complete: {output / 'summary.json'}", flush=True)


if __name__ == "__main__":
    main()
