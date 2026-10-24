package launch

import (
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

const startProblemCard = "# Start problem\n\n- TYPE: Feature\n- SIZE: small\n- TASK_GROUP: GROUP\n- LANGUAGE: en\n\n" +
	"## GOAL\n\nGoal\n\n## EXPECTED_OUTCOME\n\nOutcome\n\n## ACCEPTANCE_CRITERIA\n\n- [ ] Test\n\n" +
	"## OUT_OF_SCOPE\n\nOther work\n\n## DISCUSSION\n\nSELF_REVIEW: passed\nCARD_REVIEW: passed\n"

// useStartProblemRules serves rules to StartProblem, or err when it is set, and
// counts the configuration reads.
func useStartProblemRules(t *testing.T, rules config.Rules, err error) *int {
	t.Helper()
	reads := new(int)
	oldLoad := loadEffective
	loadEffective = func() (*config.Config, error) {
		*reads++
		if err != nil {
			return nil, err
		}
		return &config.Config{Rules: rules}, nil
	}
	t.Cleanup(func() { loadEffective = oldLoad })
	return reads
}

func TestStartProblemChecksInOrder(t *testing.T) {
	directory := board.Entry{TaskID: "20261024-start-problem-task", Path: "/board/backlog/20261024-start-problem-task", State: "backlog", Kind: "small"}
	legacy := directory
	legacy.Path += ".md"
	groupsOn := config.Rules{config.RuleTaskGroups: true, config.RuleGit: true}
	readFailed := errors.New("config read failed")
	for _, tc := range []struct {
		name      string
		entry     board.Entry
		text      string
		rules     config.Rules
		loadErr   error
		problem   string
		err       error
		wantReads int
	}{
		{"todo gate before config", directory, strings.Replace(startProblemCard, "Goal", "<FILL_IN>", 1), groupsOn, readFailed, "GOAL", nil, 0},
		{"card form before config", legacy, startProblemCard, groupsOn, readFailed, "kander init", nil, 0},
		{"task group switches", directory, startProblemCard, config.Rules{config.RuleGit: true}, nil, "GROUP", nil, 1},
		{"config read fails last", directory, startProblemCard, nil, readFailed, "", readFailed, 1},
		{"startable", directory, startProblemCard, groupsOn, nil, "", nil, 1},
		{"startable without group", directory, strings.Replace(startProblemCard, "GROUP", "", 1), nil, nil, "", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := useStartProblemRules(t, tc.rules, tc.loadErr)
			problem, err := StartProblem(tc.entry, tc.text)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v want %v", err, tc.err)
			}
			if tc.problem == "" && problem != nil || tc.problem != "" && (problem == nil || !strings.Contains(problem.Error(), tc.problem)) {
				t.Fatalf("problem %v want %q", problem, tc.problem)
			}
			if *reads != tc.wantReads {
				t.Fatalf("config reads %d want %d", *reads, tc.wantReads)
			}
		})
	}
}
