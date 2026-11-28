# Release gate shards

`release-gate.yml` runs the full v2 test suite before a release; `scripts/release.sh` tags a commit only when a run of it succeeded on that commit (see the "Releases" section of `AGENTS.md`). This page describes how the gate splits the native tests into jobs so that a run finishes in about 15 minutes.

## Terms

- **Platform**: one runner OS of the gate: `ubuntu` (`ubuntu-latest`), `macos` (`macos-latest`) or `windows` (`windows-latest`).
- **Shard**: one job of the `native` matrix. It runs a part of its platform's tests.
- **Plan**: the committed file `.github/release-gate-shards.json`. It lists the shards of every platform and what each runs.
- **Universe**: the packages a platform tests. ubuntu tests `go list ./...`. macOS and Windows test the output of `scripts/platform-test-packages.sh` plus `internal/board` and `internal/remote`.

## The plan

Each platform has:

- `parallel`: the `go test -p` value of the call that runs whole packages.
- `timeout`: the `go test -timeout` value of every call.
- `filters`: a fixed `-run` or `-skip` pattern per package. macOS skips `^TestModelCompositeStates$` in `internal/board`; Windows skips `^TestModel` in `internal/board` and runs only `^TestWindows` in `internal/remote` (`docs/model-test.md`).
- `shards`: each with a name, an estimate in seconds, and entries.

An entry names a package by its repository-relative directory and selects tests in one of three ways:

| Entry | Runs |
| --- | --- |
| `package` only | the whole package |
| `package` and `tests` | only these top-level tests |
| `package` and `skip` | the package without these tests: the owner of a split package |

A package longer than the generator's target is split: its largest tests go into `tests` entries, and one owner entry skips exactly those tests. A test that is added to the package later is in no skip list, so the owner runs it.

One shard per platform is `common`: it also runs the build, `go vet ./...`, the Import step, the concurrent lock regressions and, on Windows, the `windows_extra_packages` of a dispatch. The same shard is `rest`: it runs every package of the universe that no entry of the plan names, so a new package is tested without a new plan.

`go run ./scripts/gateshards matrix` checks the plan and prints the matrix; the gate's `plan` job runs it, so a plan that breaks a rule fails the gate before any test starts. The rules: one common and one rest shard per platform; a package is listed whole once, or split with exactly one owner whose skip list equals the union of its disjoint `tests` entries; a package with a `-run` filter is never split; a package split on one platform is whole on another. `TestCommittedPlanIsValid` checks the committed plan too.

The last rule exists for checks a `TestMain` makes over a whole-package run: `internal/server` fails when its tests together did not compare every write route, unless `-run` is set. An owner call passes `-run .`, so a split package skips that check; ubuntu never splits a package, so the check runs there in every gate.

## Running a shard

`go run ./scripts/gateshards run -platform P -shard S -universe FILE -json-dir DIR` turns the shard into `go test` calls and runs each through `scripts/go-test-json.sh`:

- The whole packages without a filter, and the rest packages, run in one call with `-p parallel`.
- A whole package with a filter, an owner and a `tests` entry each run in a call of their own with `-p 1`, because `-run` and `-skip` apply to every package of a call. An owner call adds `-run .`.
- A failing call fails the shard, and the calls after it still run.

Each call writes `DIR/<call>.json`; the job uploads the directory as the artifact `test-json-native-<os>-<shard>`.

The race job (three shards, `-race`) and the dsh job are not part of the plan.

## Regenerating the plan

Regenerate when the shard estimates drift from the job times, for example after a slow test was added:

```sh
gh run download <gate run ID> --repo dualface/kander -D /tmp/gate-artifacts
go run ./scripts/gateshards generate -artifacts /tmp/gate-artifacts -source "release-gate run <ID>"
```

The generator reads the `test-json-native-<os>-*` artifacts of the run, without the files of the common steps. A package that takes longer than the target (`-target`, 540 seconds by default) is split, except on ubuntu; then it packs the units into the fewest shards whose estimates stay within the target. A shard's estimate is its serial calls plus `max(longest package, sum of packages / 1.2)`; the common shard also carries the measured time of the common steps. A single test longer than the target gets a shard of its own.

The factor 1.2 is measured: the tests are CPU-bound, and in the first sharded run (46136634393) one ubuntu shard ran 965 seconds of serially measured package time with `-p 4` in 833 seconds; the model search alone took 650 seconds instead of 213. Every platform therefore runs two packages at a time (`parallel` 2), which also spares the timing-sensitive tests. Timings from a sharded run already contain that contention, so a plan generated from them is more cautious than one generated from a run with `-p 1`.

Some tests pass alone and fail under the load of a parallel call. `DefaultConfigs` in `scripts/gateshards/generate.go` lists them per platform under `Alone`; the generator gives each a `tests` entry of its own, which runs with `-p 1` while no other call of the shard runs, and the owner of its package skips it. `TestWindowsReadUnderContinuousAtomicReplacement` of `internal/fs` is such a test on Windows: it passed in 15 to 17 seconds in every gate run before the shards, and with `-p 2` (run 46138371949) it made 67 of the 200 file replacements it wants in 120 seconds.

After a change of the shards, compare the tests of a gate run before and after:

```sh
go run ./scripts/gateshards compare -old /tmp/gate-before -new /tmp/gate-after
```

It lists, per platform and suite (`<os> native`, `ubuntu-latest race`), the tests that only one run reported, and fails when the new run misses a test of the old one.

## Limits

GitHub runs at most 20 jobs of a public repository at a time, 5 of them on macOS. The plan, the race shards, the dsh jobs and the plan job together stay within that.
