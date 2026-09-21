// Package flow derives the task card workflow from the rule module switches and
// the configuration, without performing any action. The chart is a projection of
// the released rules: every phase, gate, exit, and back edge below cites a rule
// clause in docs/workflow-chart.md.
package flow

import "github.com/dualface/kander/internal/config"

// Phase keys, in the order the rules run them. An Exit.Target naming an earlier
// key is a back edge; naming a later key is a forward jump.
const (
	PhaseIntake        = "intake"
	PhaseCreate        = "create"
	PhaseClaim         = "claim"
	PhaseBranch        = "branch"
	PhaseImplement     = "implement"
	PhaseSelfCheck     = "self_check"
	PhaseDeliveryCheck = "delivery_check"
	PhaseDeliverGroup  = "deliver_group"
	PhaseWhitelist     = "whitelist"
	PhaseReviewPlan    = "review_plan"
	PhaseStagePrimary  = "stage_primary"
	PhaseStageSecurity = "stage_security"
	PhaseBatchClose    = "batch_close"
	PhaseUnresolved    = "unresolved"
	PhaseIntegrate     = "integrate"
	PhaseFinishOwn     = "finish_own"
	PhaseWrapUp        = "wrap_up"
	PhaseComplete      = "complete"
	PhaseReport        = "report"
)

// Review roles, in stage order.
const (
	RolePrimary  = "PMQA"
	RoleSecurity = "Security"
)

// Stage policy values. ModeInvalid marks a role whose policy could not be
// resolved; the role stays on the chart instead of being dropped silently.
const (
	ModeRequired = "required"
	ModeAuto     = "auto"
	ModeSkip     = "skip"
	ModeInvalid  = "invalid"
)

// Node is the agent and model a phase runs with. Role and Mode are empty for
// the execution node.
type Node struct {
	Role   string
	Mode   string
	Agent  string
	Model  string
	Effort string
}

// IsZero reports whether the phase has no agent of its own.
func (n Node) IsZero() bool {
	return n.Role == "" && n.Agent == "" && n.Model == "" && n.Effort == ""
}

// Exit is one labeled way out of a gate. Steps are the ordered actions on that
// exit. Target names the phase the exit leads to, empty meaning the next phase
// in order. User marks an exit the rules route to the user for a decision.
type Exit struct {
	Key    string
	Steps  []string
	Target string
	User   bool
}

// Gate is one decision with its exits.
type Gate struct {
	Key   string
	Exits []Exit
}

// Phase is one block of the workflow. Notes are annotation keys shown with the
// phase; Gates run after it, in order.
type Phase struct {
	Key   string
	Node  Node
	Notes []string
	Gates []Gate
}

// Chart is the workflow for one task scale.
type Chart struct {
	Scale  string
	Phases []Phase
}

// Phase returns the phase with that key and whether it exists.
func (c Chart) Phase(key string) (Phase, bool) {
	for _, phase := range c.Phases {
		if phase.Key == key {
			return phase, true
		}
	}
	return Phase{}, false
}

// Has reports whether the chart contains that phase.
func (c Chart) Has(key string) bool {
	_, ok := c.Phase(key)
	return ok
}

// BuildCharts returns the large then small charts.
func BuildCharts(cfg *config.Config) []Chart {
	out := make([]Chart, 0, len(config.TaskScales))
	for _, scale := range config.TaskScales {
		out = append(out, BuildChart(cfg, scale))
	}
	return out
}

// BuildChart derives the workflow for one task scale from the module switches
// and that scale's configuration.
func BuildChart(cfg *config.Config, scale string) Chart {
	on := func(name string) bool { return cfg != nil && cfg.Rules[name] }
	git := on(config.RuleGit)
	code := on(config.RuleCode)
	review := on(config.RuleReview)
	// Task groups need Git; without it every request is a single card.
	groups := on(config.RuleTaskGroups) && git

	// With the Git module off the card ends in the user's own delivery flow.
	finish := PhaseIntegrate
	if !git {
		finish = PhaseFinishOwn
	}
	// Direct execution skips the board but still follows the Git rules.
	direct := PhaseImplement
	if git {
		direct = PhaseBranch
	}

	chart := Chart{Scale: scale}
	add := func(phases ...Phase) {
		chart.Phases = append(chart.Phases, phases...)
	}

	if on(config.RuleTaskIntake) {
		add(intakePhase(direct))
	}
	add(createPhase(scale, groups), claimPhase())
	if git {
		add(branchPhase(groups))
	}
	add(implementPhase(cfg, scale, git))
	// The delivery self-check runs before a card enters review/, or when a
	// single card requests review; review/ itself belongs to task groups only.
	switch {
	case code && (review || groups):
		add(selfCheckPhase())
	case !code && review:
		add(deliveryCheckPhase())
	}
	if groups {
		add(deliverGroupPhase())
	}
	if review {
		primary := reviewNode(cfg, scale, RolePrimary)
		security := reviewNode(cfg, scale, RoleSecurity)
		add(
			whitelistPhase(finish),
			reviewPlanPhase(groups),
			primaryPhase(primary, security, groups),
			securityPhase(security, primary, groups, git),
			batchClosePhase(),
			unresolvedPhase(),
		)
	}
	if git {
		add(integratePhase(review, groups))
	} else {
		add(finishOwnPhase(review))
	}
	if groups {
		add(wrapUpPhase())
	}
	add(completePhase(scale))
	if on(config.RuleReporting) {
		add(reportPhase())
	}
	return chart
}

// intakePhase is the execution-choice gate: five options, the fifth looping
// back to replanning.
func intakePhase(direct string) Phase {
	return Phase{Key: PhaseIntake, Gates: []Gate{{
		Key: "gate_intake",
		Exits: []Exit{
			{Key: "intake_board_self", Target: PhaseCreate, User: true},
			{Key: "intake_board_handoff", Target: PhaseCreate, User: true},
			{Key: "intake_board_later", Target: PhaseCreate, User: true},
			{Key: "intake_direct", Target: direct, User: true},
			{Key: "intake_adjust", Target: PhaseIntake, User: true},
		},
	}}}
}

// createPhase covers kander new, the contract, the self-review, and the todo
// gate. Large cards and group members also need an independent card review.
func createPhase(scale string, groups bool) Phase {
	notes := []string{"note_self_review"}
	if scale == "large" || groups {
		notes = append(notes, "note_card_review")
	}
	return Phase{Key: PhaseCreate, Notes: notes, Gates: []Gate{{
		Key: "gate_todo",
		Exits: []Exit{
			{Key: "todo_incomplete", Target: PhaseCreate},
			{Key: "todo_ready"},
		},
	}}}
}

func claimPhase() Phase {
	return Phase{Key: PhaseClaim, Gates: []Gate{{
		Key: "gate_claim",
		Exits: []Exit{
			{Key: "claim_start"},
			{Key: "claim_self"},
		},
	}}}
}

func branchPhase(groups bool) Phase {
	notes := []string{"note_overlap"}
	if groups {
		notes = append(notes, "note_group_branch")
	}
	return Phase{Key: PhaseBranch, Notes: notes}
}

func implementPhase(cfg *config.Config, scale string, git bool) Phase {
	notes := []string{"note_smallest_change"}
	if git {
		notes = append(notes, "note_commit_by_concern")
	}
	return Phase{Key: PhaseImplement, Node: executionNode(cfg, scale), Notes: notes}
}

// selfCheckPhase is the seven-item delivery self-check with its command gate.
func selfCheckPhase() Phase {
	return Phase{Key: PhaseSelfCheck, Notes: []string{"note_self_check_items"}, Gates: []Gate{{
		Key: "gate_delivery",
		Exits: []Exit{
			{Key: "delivery_fail", Target: PhaseImplement},
			{Key: "delivery_review_required", Steps: []string{"delivery_record_disposition"}},
			{Key: "delivery_incomplete", Target: PhaseImplement},
			{Key: "delivery_pass"},
		},
	}}}
}

// deliveryCheckPhase is the reduced check the review rules require when the
// code module is off.
func deliveryCheckPhase() Phase {
	return Phase{Key: PhaseDeliveryCheck, Notes: []string{"note_delivery_check_minimal"}, Gates: []Gate{{
		Key: "gate_delivery",
		Exits: []Exit{
			{Key: "delivery_fail", Target: PhaseImplement},
			{Key: "delivery_incomplete", Target: PhaseImplement},
			{Key: "delivery_pass"},
		},
	}}}
}

// deliverGroupPhase is the task group delivery onto the group branch.
func deliverGroupPhase() Phase {
	return Phase{Key: PhaseDeliverGroup, Notes: []string{"note_group_delivery"}, Gates: []Gate{{
		Key: "gate_group_receive",
		Exits: []Exit{
			{Key: "group_ff_conflict", Steps: []string{"group_notify_sync"}, Target: PhaseImplement},
			{Key: "group_received"},
		},
	}}}
}

// whitelistPhase is the review trigger whitelist; a miss notifies the user and
// continues straight into the finish without waiting for a reply.
func whitelistPhase(finish string) Phase {
	return Phase{Key: PhaseWhitelist, Notes: []string{"note_whitelist_entries"}, Gates: []Gate{{
		Key: "gate_whitelist",
		Exits: []Exit{
			{Key: "whitelist_miss", Steps: []string{"whitelist_notice_scope"}, Target: finish},
			{Key: "whitelist_hit"},
		},
	}}}
}

func reviewPlanPhase(groups bool) Phase {
	notes := []string{"note_plan_requirements"}
	if groups {
		notes = append(notes, "note_batch_unit")
	}
	return Phase{Key: PhaseReviewPlan, Notes: notes}
}

// primaryPhase is stage one. An auto PMQA always runs once review is triggered,
// because every whitelist entry resolves to a profile containing PMQA, so the
// stage has no trigger-condition gate.
func primaryPhase(node, security Node, groups bool) Phase {
	phase := Phase{Key: PhaseStagePrimary, Node: node, Notes: stageNotes(node.Mode)}
	switch node.Mode {
	case ModeSkip, ModeInvalid:
		return phase
	}
	fix := []string{"mf_contract_ask", "mf_fix_commit"}
	if groups {
		fix = append(fix, "mf_dispatch_back")
	}
	fix = append(fix, "mf_incremental")

	phase.Gates = append(phase.Gates,
		reviewerGate("backend_stop_integration"),
		Gate{Key: "gate_must_fix", Exits: []Exit{
			{Key: "mf_none", Steps: []string{"mf_nonblocking_disposition"}},
			{Key: "mf_unverifiable", Steps: []string{"mf_stage_holds"}, User: true},
			{Key: "mf_attribution", Steps: []string{"mf_attribution_decide"}, User: true},
			{Key: "mf_mechanical", Steps: []string{"mf_mechanical_fix", "mf_mechanical_verify", "mf_advance_no_rerun"}},
			{Key: "mf_other", Steps: fix, Target: PhaseStagePrimary},
			{Key: "mf_round_cap", Steps: roundCapOptions(), User: true},
		}},
	)
	// A stage-one fix that substantively changes security code returns to the
	// security role for the fix scope.
	if security.Mode != ModeSkip && security.Mode != ModeInvalid {
		phase.Gates = append(phase.Gates, Gate{Key: "gate_cross_security", Exits: []Exit{
			{Key: "cross_security_touched", Steps: []string{"cross_security_scope"}, Target: PhaseStageSecurity},
			{Key: "cross_security_none"},
		}})
	}
	return phase
}

// securityPhase is stage two, which runs after stage one passes.
func securityPhase(node, primary Node, groups, git bool) Phase {
	phase := Phase{Key: PhaseStageSecurity, Node: node, Notes: stageNotes(node.Mode)}
	switch node.Mode {
	case ModeSkip, ModeInvalid:
		return phase
	case ModeAuto:
		// Only the security role has trigger conditions of its own.
		phase.Gates = append(phase.Gates, Gate{Key: "gate_security_conditions", Exits: []Exit{
			{Key: "security_conditions_miss", Steps: []string{"security_conditions_na"}},
			{Key: "security_conditions_hit"},
		}})
	}
	fix := []string{"sec_decision_fix"}
	if groups {
		fix = append(fix, "mf_dispatch_back")
	}
	fix = append(fix, "sec_incremental")

	stop := "sec_decision_stop"
	if !git {
		stop = "sec_decision_stop_delivery"
	}
	exits := []Exit{
		{Key: "sec_none", Steps: []string{"sec_other_tiers"}},
		{Key: "sec_fix", Steps: fix, Target: PhaseStageSecurity, User: true},
	}
	// The security fix scope is re-reviewed by an enabled PMQA unconditionally.
	if primary.Mode != ModeSkip && primary.Mode != ModeInvalid {
		exits = append(exits, Exit{Key: "sec_cross_primary", Steps: []string{"sec_cross_scope"}, Target: PhaseStagePrimary})
	}
	exits = append(exits,
		Exit{Key: "sec_accept", Steps: []string{"sec_accept_record"}, User: true},
		Exit{Key: stop, User: true},
		Exit{Key: "sec_timeout", Steps: []string{"sec_timeout_blocking_waits"}},
		Exit{Key: "sec_round_cap", Steps: roundCapOptions(), User: true},
	)
	phase.Gates = append(phase.Gates,
		reviewerGate("backend_security_partial"),
		Gate{Key: "gate_security_qualified", Exits: exits},
	)
	return phase
}

// stageNotes spells out what the stage policy means for that role.
func stageNotes(mode string) []string {
	switch mode {
	case ModeRequired:
		return []string{"note_mode_required"}
	case ModeAuto:
		return []string{"note_mode_auto"}
	case ModeSkip:
		return []string{"note_stage_na", "note_stage_na_override"}
	default:
		return []string{"note_stage_invalid"}
	}
}

// reviewerGate is the run gate both stages share: the reviewer CLI may be
// unavailable, and a persistent backend failure has a per-stage consequence.
func reviewerGate(backend string) Gate {
	return Gate{Key: "gate_reviewer_ran", Exits: []Exit{
		{Key: "reviewer_unavailable", Steps: []string{
			"reviewer_opt_install", "reviewer_opt_switch", "reviewer_opt_skip", "reviewer_opt_stop",
		}, User: true},
		{Key: "reviewer_backend_failure", Steps: []string{backend}, User: true},
		{Key: "reviewer_ran"},
	}}
}

func roundCapOptions() []string {
	return []string{"cap_reason", "cap_opt_accept", "cap_opt_contract", "cap_opt_one_more"}
}

func batchClosePhase() Phase {
	return Phase{Key: PhaseBatchClose, Notes: []string{"note_batch_close"}}
}

func unresolvedPhase() Phase {
	return Phase{Key: PhaseUnresolved, Notes: []string{"note_unresolved_items"}}
}

// integratePhase is the one-time review gate plus integration and cleanup.
func integratePhase(review, groups bool) Phase {
	phase := Phase{Key: PhaseIntegrate, Notes: []string{"note_integration_auth"}}
	if review {
		exits := []Exit{
			{Key: "rebase_clean", Steps: []string{"rebase_reverify"}},
			{Key: "rebase_markdown_only"},
			{Key: "rebase_code_conflict", Steps: []string{"rebase_reverify"}, Target: PhaseStagePrimary},
		}
		if groups {
			// A task group never re-reviews a rebase: its patch must stay
			// identical, and any conflict is integrated by a merge commit.
			exits = []Exit{
				{Key: "group_patch_equal"},
				{Key: "group_patch_changed", Steps: []string{"group_merge_commit", "group_merge_options"}, Target: PhaseReviewPlan, User: true},
				{Key: "rebase_code_conflict", Steps: []string{"rebase_reverify"}, Target: PhaseStagePrimary},
			}
		}
		phase.Gates = append(phase.Gates, Gate{Key: "gate_rebase", Exits: exits})
	}
	phase.Notes = append(phase.Notes, "note_integration_verify", "note_cleanup")
	return phase
}

// finishOwnPhase replaces integration when the Git module is off.
func finishOwnPhase(review bool) Phase {
	notes := []string{"note_finish_own"}
	if review {
		notes = append(notes, "note_base_from_start")
	}
	return Phase{Key: PhaseFinishOwn, Notes: notes}
}

func wrapUpPhase() Phase {
	return Phase{Key: PhaseWrapUp, Notes: []string{"note_wrap_up"}}
}

func completePhase(scale string) Phase {
	notes := []string{"note_summary"}
	if scale == "large" {
		notes = []string{"note_report_md"}
	}
	return Phase{Key: PhaseComplete, Notes: append(notes, "note_evidence_gate", "note_move_done")}
}

func reportPhase() Phase {
	return Phase{Key: PhaseReport, Notes: []string{"note_report_fields"}}
}

func executionNode(cfg *config.Config, scale string) Node {
	if cfg == nil {
		return Node{}
	}
	agent := cfg.KanbanAgents[scale]
	entry := cfg.Models.Kanban[agent]
	node := Node{Agent: agent, Model: config.KanbanModelFor(entry, scale)}
	if config.AgentSupportsEffort(cfg, agent) {
		node.Effort = entry[scale+"_effort"]
	}
	return node
}

func reviewNode(cfg *config.Config, scale, role string) Node {
	node := Node{Role: role, Mode: ModeInvalid}
	if cfg == nil {
		return node
	}
	// An unreadable or unknown policy keeps the role on the chart as invalid
	// rather than dropping it silently.
	if mode, err := config.ReviewStageFor(cfg, scale, role); err == nil {
		switch mode {
		case ModeRequired, ModeAuto, ModeSkip:
			node.Mode = mode
		}
	}
	agent := config.ReviewerFor(cfg, scale, role)
	model, effort := config.ReviewModelFor(cfg, agent, role, scale)
	// Review launch keeps effort only when the agent's review template takes one.
	if !config.ReviewModelSupportsEffort(cfg, agent) {
		effort = ""
	}
	node.Agent = agent
	node.Model = model
	node.Effort = effort
	return node
}
