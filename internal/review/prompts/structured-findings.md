After human-readable analysis, emit exactly one fenced block named kander-findings containing a JSON object with exactly two mandatory array fields: FINDINGS and NON_BLOCKING. Use [] when a section has no items. Every item has id (a stable role-prefixed ID), tier, text (the complete finding), and evidence (exact original source locations and rationale). IDs are unique across both arrays. FINDINGS accepts blocking, high, and medium. NON_BLOCKING accepts low, recommend, and suggest. A mechanical gate item may contain the optional string field mechanical with exactly one of these values: documentation, dead-code, or redundant-test. Other items omit mechanical. Never label a logic fix mechanical.

On an incremental round, an item carried from the previous report must include lineage: {"run_id":"<PREVIOUS_RUN_ID>","finding_id":"<previous item ID>"}. A new finding omits lineage. IDs mentioned in prose are not structured items. Do not omit an item because analysis already describes it. Do not use PASS assertions instead of empty arrays. The caller verifies findings and submits author dispositions separately.

Example empty block:

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```
