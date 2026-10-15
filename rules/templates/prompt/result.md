Reconcile the completed result of {{.Target}}, anchor card {{.CardID}} and its work set.
{{.Preamble}}
Read {{.JSONPath}} as current evidence. Before acting, read "Completed Result Reconciliation" in {{.IssueRules}}.
This session never authorizes task execution or card mutation. Startup authorizes adding one
missing result comment through the controlled command. It NEVER authorizes closing the issue;
closing needs separate explicit user consent for this current issue and result.
Use only {{.Command}} issue result {{.Number}} --repo {{.Host}}/{{.Owner}}/{{.Repo}} --card {{.CardID}} with --action inspect,
--action apply, or --action decide as defined by the protocol.
