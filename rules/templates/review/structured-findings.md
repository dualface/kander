Return the complete review report with every finding, its severity, exact source evidence, and reasoning. Explicitly state when there are no findings in either the gate or non-blocking section. The receiver reads the complete report and is responsible for interpreting its meaning; missing or unreadable content never means zero findings.

For automatic extraction, prefer exactly one fenced block named kander-findings containing a JSON object with the two array fields FINDINGS and NON_BLOCKING. Put each fence on its own line, with no trailing text. Use [] when a section has no items. Every item has id (a stable role-prefixed ID), tier, text (the complete finding), and evidence (exact original source locations and rationale). IDs are unique across both arrays. FINDINGS accepts blocking, high, and medium. NON_BLOCKING accepts low, recommend, and suggest. A mechanical gate item may contain the optional string field mechanical with exactly one of these values: documentation, dead-code, or redundant-test. Other items omit mechanical. Never label a logic fix mechanical.

On an incremental round, identify each carried item by its immediate predecessor run and finding ID; in JSON use lineage: {"run_id":"<PREVIOUS_RUN_ID>","finding_id":"<previous item ID>"}. A new finding omits lineage. Do not omit an item because analysis already describes it. The caller verifies findings and submits author dispositions separately.

A complete readable report without this exact formatting is retained for explicit receiver interpretation. This does not permit omissions or weaken evidence, lineage, or disposition requirements.

Example empty block:

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```
