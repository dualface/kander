package review

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// reviewTemplatesDir holds the reviewer prompt templates below the rules root; roles/*.md
// include each other, so the directory is rendered as one template set.
const reviewTemplatesDir = "templates/review"

type reviewContractData struct {
	Role                      string
	Scope                     string
	RoleRules                 string
	TierRules                 string
	StructuredFindingRules    string
	InspectionRules           string
	ReportLanguageRule        string
	LastMessageOutputContract string
}

type reviewBootstrapData struct {
	Role                   string
	Root                   string
	Base                   string
	Commit                 string
	EvidenceFile           string
	ContractFile           string
	TaskContext            string
	AutomaticReviewContext string
	CallerReviewContext    string
}

type incrementalScopeData struct {
	Base     string
	Commit   string
	Reviewed string
}

func renderReviewTemplate(name string, data any) (string, error) {
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		paths = config.InstallPaths{}
	}
	out, err := config.RenderRulesTemplateSet(paths, reviewTemplatesDir, name, data, os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out) + "\n", nil
}

var roleTemplates = map[string]string{
	"PMQA":     "role-pmqa.md",
	"Security": "role-security.md",
	"PM":       "role-pm.md",
	"QA":       "role-qa.md",
	"CSA":      "role-csa.md",
	"Hacker":   "role-hacker.md",
}

// roleRules renders the review-focus text of one role; an unknown role has none.
func roleRules(role string) (string, error) {
	name, ok := roleTemplates[role]
	if !ok {
		return "", nil
	}
	return renderReviewTemplate(name, nil)
}

func lastMessageOutputContract() (string, error) {
	text, err := renderReviewTemplate("last-message-output.md", nil)
	return strings.TrimSpace(text), err
}

func incrementalScopeRules(ctx reviewContext) (string, error) {
	return renderReviewTemplate("scope-incremental.md", incrementalScopeData{
		Base: ctx.base, Commit: ctx.commit, Reviewed: ctx.reviewed,
	})
}

func buildReviewContract(ctx reviewContext) (string, error) {
	scope, err := renderReviewTemplate("scope-full.md", nil)
	if ctx.reviewed != "" {
		scope, err = incrementalScopeRules(ctx)
	}
	if err != nil {
		return "", err
	}
	outputContract := ""
	if usesLastMessageReport(ctx.agent) {
		if outputContract, err = lastMessageOutputContract(); err != nil {
			return "", err
		}
	}
	roles, err := roleRules(ctx.role)
	if err != nil {
		return "", err
	}
	tiers, err := renderReviewTemplate("tiers.md", nil)
	if err != nil {
		return "", err
	}
	findings, err := renderReviewTemplate("structured-findings.md", nil)
	if err != nil {
		return "", err
	}
	language, err := reportLanguageRule(ctx.reportLanguage)
	if err != nil {
		return "", err
	}
	return renderReviewTemplate("review-contract.md", reviewContractData{
		Role:                      ctx.role,
		Scope:                     scope,
		RoleRules:                 roles,
		TierRules:                 tiers,
		StructuredFindingRules:    findings,
		InspectionRules:           ctx.settings.inspectionRules,
		ReportLanguageRule:        language,
		LastMessageOutputContract: outputContract,
	})
}

func reportLanguageRule(language string) (string, error) {
	if language == "" {
		return "", nil
	}
	return renderReviewTemplate("report-language.md", struct{ Language string }{Language: language})
}

func buildPrompt(ctx reviewContext, evidenceFile, taskContext string) (string, error) {
	automatic, caller := reviewPromptContext(ctx)
	return renderReviewTemplate("bootstrap.md", reviewBootstrapData{
		Role:                   ctx.role,
		Root:                   ctx.root,
		Base:                   ctx.base,
		Commit:                 ctx.commit,
		EvidenceFile:           evidenceFile,
		ContractFile:           filepath.Join(filepath.Dir(evidenceFile), config.ReviewContractFilename),
		TaskContext:            taskContext,
		AutomaticReviewContext: automatic,
		CallerReviewContext:    caller,
	})
}

func usesLastMessageReport(agent string) bool {
	switch agent {
	case "claude", "cursor", "grok", "kimi", "droid":
		return true
	default:
		return false
	}
}

// reviewPromptContext unwraps the producer framing while preserving every byte
// of the automatic and caller-supplied contexts.
func reviewPromptContext(ctx reviewContext) (automatic, caller string) {
	if ctx.archive == nil || ctx.archive.run.PreviousRunID == "" {
		return "", ctx.reviewContext
	}
	source, supplement, err := board.SplitReviewContext([]byte(ctx.reviewContext))
	if err != nil {
		return "", ctx.reviewContext
	}
	return string(source), supplement
}
