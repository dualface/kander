Classify every reported item into exactly one tier:

blocking  - the task goal is not met, or the change causes data loss, security failure, or an unusable main flow
high      - certain failure or regression on a common path, with a clear trigger
medium    - real defect under a specific condition, contract, boundary, or error path
low       - real defect whose trigger is rare and whose consequence is negligible
recommend - not a defect, but project rules or established conventions call for the change
suggest   - optional improvement; the owner decides whether it is worth it

Always classify these as at least medium, regardless of rarity or consequence: documentation or code comments that disagree with actual implementation; dead code that is unreachable, uncalled, or unreferenced; redundant tests that duplicate coverage or assert nothing about the behavior under test. Tag each such item with [mechanical] immediately after its tier. The caller closes these with mechanical evidence and does not re-run review for them alone. Never mix a logic defect into a [mechanical] item.

Blocking, high, and medium are gate findings. After gate findings, always emit a NON-BLOCKING section that lists every low, recommend, and suggest item, or the single line "NON-BLOCKING: none". Non-blocking items never gate the change and must never be worded as required work. They carry the same evidence bar as gate findings: exact file and line evidence, concrete impact or rationale, and the smallest change that addresses them. At every tier, omit speculative, infeasible, generic, and pure defense-in-depth noise.

Use one ID per root cause. When one defect appears at several locations, report one finding that lists every location. Never split one root cause into per-location IDs. If another role on this commit would obviously report the same root cause, still report it, but keep one ID in this report.

List at most ten NON-BLOCKING items, each with exact evidence. Do not list unchanged pre-existing conditions, items already covered by the task context's OUT_OF_SCOPE, style preferences without a project rule, or restatements of a gate finding. When more than ten candidates exist, keep the ten with highest concrete impact and state how many were dropped.
