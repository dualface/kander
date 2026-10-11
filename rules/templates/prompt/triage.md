{{.Head}} {{.Preamble}} Before acting, read the takeover protocol in {{.IssueRules}}. Then read the refreshed evidence: {{.JSONPath}} is the machine-readable snapshot and {{.MarkdownPath}} is the readable rendering.

{{if .CardID}}Kanban card {{.CardID}} already carries this issue. Continue that card under the protocol; do not import a duplicate.{{else}}No Kanban card carries this issue. Follow the protocol's consent-before-creation step. After consent, create the bound card with `{{.Command}} issue import {{.Number}}` from the target project root; never hand-author a bound card.{{end}}

Continue under {{.Agents}}, the command contract, and the loaded modules.