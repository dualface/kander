package flow

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func testConfig(rules map[string]bool) *config.Config {
	cfg := config.DefaultConfig()
	for name, value := range rules {
		cfg.Rules[name] = value
	}
	return cfg
}

func allRules(on bool) map[string]bool {
	out := map[string]bool{}
	for _, name := range config.RuleModules {
		out[name] = on
	}
	return out
}

func phaseKeys(chart Chart) []string {
	keys := make([]string, 0, len(chart.Phases))
	for _, phase := range chart.Phases {
		keys = append(keys, phase.Key)
	}
	return keys
}

// exitOf returns the exit with that key anywhere in the chart.
func exitOf(chart Chart, key string) (Exit, bool) {
	for _, phase := range chart.Phases {
		for _, gate := range phase.Gates {
			for _, exit := range gate.Exits {
				if exit.Key == key {
					return exit, true
				}
			}
		}
	}
	return Exit{}, false
}

func gateOf(chart Chart, phase, key string) bool {
	p, ok := chart.Phase(phase)
	if !ok {
		return false
	}
	for _, gate := range p.Gates {
		if gate.Key == key {
			return true
		}
	}
	return false
}

// TestPhaseMatrix pins which module switch owns which phase.
func TestPhaseMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rules   map[string]bool
		present []string
		absent  []string
	}{
		{"all on", allRules(true),
			[]string{PhaseIntake, PhaseCreate, PhaseClaim, PhaseBranch, PhaseImplement, PhaseSelfCheck,
				PhaseDeliverGroup, PhaseWhitelist, PhaseReviewPlan, PhaseStagePrimary, PhaseStageSecurity,
				PhaseBatchClose, PhaseUnresolved, PhaseIntegrate, PhaseWrapUp, PhaseComplete, PhaseReport},
			[]string{PhaseFinishOwn, PhaseDeliveryCheck}},
		{"all off", allRules(false),
			[]string{PhaseCreate, PhaseClaim, PhaseImplement, PhaseFinishOwn, PhaseComplete},
			[]string{PhaseIntake, PhaseBranch, PhaseSelfCheck, PhaseDeliveryCheck, PhaseDeliverGroup,
				PhaseWhitelist, PhaseStagePrimary, PhaseStageSecurity, PhaseIntegrate, PhaseWrapUp, PhaseReport}},
		{"git off drops groups", map[string]bool{config.RuleGit: false},
			[]string{PhaseFinishOwn}, []string{PhaseBranch, PhaseIntegrate, PhaseDeliverGroup, PhaseWrapUp}},
		{"reporting off", map[string]bool{config.RuleReporting: false},
			[]string{PhaseComplete}, []string{PhaseReport}},
		{"intake off", map[string]bool{config.RuleTaskIntake: false},
			[]string{PhaseCreate}, []string{PhaseIntake}},
		{"review off", map[string]bool{config.RuleReview: false},
			[]string{PhaseImplement, PhaseIntegrate},
			[]string{PhaseWhitelist, PhaseReviewPlan, PhaseStagePrimary, PhaseStageSecurity, PhaseBatchClose, PhaseUnresolved}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := allRules(true)
			for name, value := range tc.rules {
				rules[name] = value
			}
			chart := BuildChart(testConfig(rules), "large")
			for _, key := range tc.present {
				if !chart.Has(key) {
					t.Fatalf("missing phase %q in %v", key, phaseKeys(chart))
				}
			}
			for _, key := range tc.absent {
				if chart.Has(key) {
					t.Fatalf("unexpected phase %q in %v", key, phaseKeys(chart))
				}
			}
		})
	}
}

// TestDeliverySelfCheckTriggers covers the real triggers of the self-check:
// the code module decides its content, and a card reaches it by entering
// review/ (task groups) or by requesting review.
func TestDeliverySelfCheckTriggers(t *testing.T) {
	for _, tc := range []struct {
		name               string
		code, review, grp  bool
		selfCheck, reduced bool
	}{
		{"code and review", true, true, false, true, false},
		{"code and groups only", true, false, true, true, false},
		{"code alone", true, false, false, false, false},
		{"review without code", false, true, false, false, true},
		{"neither", false, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := allRules(true)
			rules[config.RuleCode] = tc.code
			rules[config.RuleReview] = tc.review
			rules[config.RuleTaskGroups] = tc.grp
			chart := BuildChart(testConfig(rules), "large")
			if got := chart.Has(PhaseSelfCheck); got != tc.selfCheck {
				t.Fatalf("self check=%v want %v", got, tc.selfCheck)
			}
			if got := chart.Has(PhaseDeliveryCheck); got != tc.reduced {
				t.Fatalf("reduced check=%v want %v", got, tc.reduced)
			}
		})
	}
}

// TestStagePolicies pins the per-role stage semantics, including that stage one
// never gets a trigger-condition gate of its own.
func TestStagePolicies(t *testing.T) {
	cfg := testConfig(allRules(true))
	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeAuto, "Security": ModeAuto}
	chart := BuildChart(cfg, "large")
	if gateOf(chart, PhaseStagePrimary, "gate_security_conditions") {
		t.Fatal("stage one must not have trigger conditions of its own")
	}
	if !gateOf(chart, PhaseStageSecurity, "gate_security_conditions") {
		t.Fatal("an auto security stage needs its trigger-condition gate")
	}

	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeRequired, "Security": ModeRequired}
	chart = BuildChart(cfg, "large")
	if gateOf(chart, PhaseStageSecurity, "gate_security_conditions") {
		t.Fatal("required overrides the trigger conditions")
	}

	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeRequired, "Security": ModeSkip}
	chart = BuildChart(cfg, "large")
	security, _ := chart.Phase(PhaseStageSecurity)
	if len(security.Gates) != 0 || len(security.Notes) == 0 {
		t.Fatal("a skipped stage is N/A with its override note and no gates")
	}
	if _, ok := exitOf(chart, "cross_security_touched"); ok {
		t.Fatal("no cross edge to a skipped security stage")
	}

	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeSkip, "Security": ModeRequired}
	chart = BuildChart(cfg, "large")
	if _, ok := exitOf(chart, "sec_cross_primary"); ok {
		t.Fatal("no cross edge to a skipped primary stage")
	}
}

// TestInvalidStagePolicyStaysVisible covers that an unreadable policy keeps the
// role on the chart instead of dropping it silently.
func TestInvalidStagePolicyStaysVisible(t *testing.T) {
	cfg := testConfig(allRules(true))
	cfg.ReviewStages["large"] = map[string]string{"PMQA": "nonsense", "Security": ModeRequired}
	chart := BuildChart(cfg, "large")
	primary, ok := chart.Phase(PhaseStagePrimary)
	if !ok || primary.Node.Mode != ModeInvalid {
		t.Fatalf("mode=%q want %q", primary.Node.Mode, ModeInvalid)
	}
	if len(primary.Gates) != 0 {
		t.Fatal("an unreadable policy gates nothing")
	}
}

// TestBackEdgeTargets pins every loop the rules require.
func TestBackEdgeTargets(t *testing.T) {
	cfg := testConfig(allRules(true))
	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeRequired, "Security": ModeRequired}
	chart := BuildChart(cfg, "large")
	for _, want := range []struct{ exit, target string }{
		{"intake_adjust", PhaseIntake},
		{"todo_incomplete", PhaseCreate},
		{"delivery_fail", PhaseImplement},
		{"delivery_incomplete", PhaseImplement},
		{"group_ff_conflict", PhaseImplement},
		{"whitelist_miss", PhaseIntegrate},
		{"mf_other", PhaseStagePrimary},
		{"cross_security_touched", PhaseStageSecurity},
		{"sec_fix", PhaseStageSecurity},
		{"sec_cross_primary", PhaseStagePrimary},
		{"rebase_code_conflict", PhaseStagePrimary},
		{"group_patch_changed", PhaseReviewPlan},
	} {
		exit, ok := exitOf(chart, want.exit)
		if !ok {
			t.Fatalf("missing exit %q", want.exit)
		}
		if exit.Target != want.target {
			t.Fatalf("%s target=%q want %q", want.exit, exit.Target, want.target)
		}
	}
	// A single card re-reviews a hand-resolved rebase conflict; a task group
	// integrates a merge commit instead of rebasing its reviewed patch.
	single := allRules(true)
	single[config.RuleTaskGroups] = false
	solo := BuildChart(testConfig(single), "large")
	if _, ok := exitOf(solo, "group_patch_changed"); ok {
		t.Fatal("the group patch gate belongs to task groups only")
	}
	if exit, ok := exitOf(solo, "rebase_code_conflict"); !ok || exit.Target != PhaseStagePrimary {
		t.Fatal("a single card re-reviews a hand-resolved code conflict")
	}
}

// TestUserDecisionExits pins the exits the rules route to the user.
func TestUserDecisionExits(t *testing.T) {
	cfg := testConfig(allRules(true))
	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeRequired, "Security": ModeRequired}
	chart := BuildChart(cfg, "large")
	for _, key := range []string{
		"mf_unverifiable", "mf_attribution", "mf_round_cap",
		"reviewer_unavailable", "reviewer_backend_failure",
		"sec_fix", "sec_accept", "sec_decision_stop", "sec_round_cap",
		"group_patch_changed",
	} {
		exit, ok := exitOf(chart, key)
		if !ok {
			t.Fatalf("missing exit %q", key)
		}
		if !exit.User {
			t.Fatalf("%q must be marked as a user decision", key)
		}
	}
	// A confirmed blocking or high finding always waits; the timeout is a
	// medium-only rule and is not itself a user decision.
	timeout, ok := exitOf(chart, "sec_timeout")
	if !ok || timeout.User || len(timeout.Steps) == 0 {
		t.Fatal("the timeout exit states that blocking and high still wait")
	}
}

// TestGitOffReplacesIntegration covers the delivery end when Git is disabled.
func TestGitOffReplacesIntegration(t *testing.T) {
	rules := allRules(true)
	rules[config.RuleGit] = false
	chart := BuildChart(testConfig(rules), "large")
	if exit, ok := exitOf(chart, "whitelist_miss"); !ok || exit.Target != PhaseFinishOwn {
		t.Fatal("a whitelist miss must reach the user's own finish")
	}
	if _, ok := exitOf(chart, "sec_decision_stop_delivery"); !ok {
		t.Fatal("without Git the third security option stops delivery")
	}
	if _, ok := exitOf(chart, "sec_decision_stop"); ok {
		t.Fatal("integration cannot be stopped when there is none")
	}
}

// TestScalesDiffer covers that the two scales project their own configuration.
func TestScalesDiffer(t *testing.T) {
	cfg := testConfig(allRules(true))
	cfg.ReviewStages["large"] = map[string]string{"PMQA": ModeRequired, "Security": ModeRequired}
	cfg.ReviewStages["small"] = map[string]string{"PMQA": ModeRequired, "Security": ModeAuto}
	charts := BuildCharts(cfg)
	if len(charts) != len(config.TaskScales) {
		t.Fatalf("charts=%d", len(charts))
	}
	large, small := charts[0], charts[1]
	if large.Scale != "large" || small.Scale != "small" {
		t.Fatalf("scales %q %q", large.Scale, small.Scale)
	}
	if gateOf(large, PhaseStageSecurity, "gate_security_conditions") {
		t.Fatal("large security is required here")
	}
	if !gateOf(small, PhaseStageSecurity, "gate_security_conditions") {
		t.Fatal("small security is auto here and needs its trigger gate")
	}
	// The execution node follows the scale's agent and model.
	cfg.KanbanAgents["large"] = "codex"
	cfg.KanbanAgents["small"] = "claude"
	cfg.Models.Kanban["codex"]["large_model"] = "large-only"
	cfg.Models.Kanban["claude"]["small_model"] = "small-only"
	charts = BuildCharts(cfg)
	largeNode, _ := charts[0].Phase(PhaseImplement)
	smallNode, _ := charts[1].Phase(PhaseImplement)
	if largeNode.Node.Model != "large-only" || smallNode.Node.Model != "small-only" {
		t.Fatalf("models %q %q", largeNode.Node.Model, smallNode.Node.Model)
	}
}

// TestCardReviewNote covers the independent card review large cards and group
// members need before todo/.
func TestCardReviewNote(t *testing.T) {
	rules := allRules(true)
	rules[config.RuleTaskGroups] = false
	large, _ := BuildChart(testConfig(rules), "large").Phase(PhaseCreate)
	small, _ := BuildChart(testConfig(rules), "small").Phase(PhaseCreate)
	if !strings.Contains(strings.Join(large.Notes, " "), "note_card_review") {
		t.Fatal("a large card needs the independent card review")
	}
	if strings.Contains(strings.Join(small.Notes, " "), "note_card_review") {
		t.Fatal("a standalone small card needs no independent card review")
	}
	grouped, _ := BuildChart(testConfig(allRules(true)), "small").Phase(PhaseCreate)
	if !strings.Contains(strings.Join(grouped.Notes, " "), "note_card_review") {
		t.Fatal("a group member needs the independent card review at any scale")
	}
}

// TestNilConfigIsSafe covers the read path with no configuration at all.
func TestNilConfigIsSafe(t *testing.T) {
	chart := BuildChart(nil, "large")
	if len(chart.Phases) == 0 {
		t.Fatal("a chart without configuration still shows the fixed phases")
	}
	if chart.Has(PhaseStagePrimary) || chart.Has(PhaseIntegrate) {
		t.Fatal("no module is enabled without configuration")
	}
}
