Fixes included this round: comment-only. version.go and AGENTS.md no longer call the injected value a semantic version; they describe release-tag injection vs local git describe / dev. Commit 96ddc888e30a399ddbf23b19d4818cfe3ac49ed1.

Review focus:
(1) Contract: version.String() returns the injected Version or dev; callers unchanged.
(2) Release gate: tags ['v*'], VERSION from GITHUB_REF_NAME#v, tag_name is github.ref_name, packaging unchanged.
(3) Local make: git describe --tags --always when tags exist, else dev.

Verification records:
- go test ./internal/version ./internal/cli -count=1 at 96ddc888e30a399ddbf23b19d4818cfe3ac49ed1: ok
- earlier go vet ./... && go test ./... -count=1 at 5172edad0c23c2f85265461d6238215f443849a9: 21 packages ok
- make VERSION=0.5.0 => kander 0.5.0; non-git and no-tag describe fallback => dev

After the analysis, the report MUST end with exactly one fenced block:

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```

Fill the arrays. A missing fence fails the gate. Do not put prose after the fence.