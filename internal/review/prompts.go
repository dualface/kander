package review

import (
	"bytes"
	"embed"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dualface/kander/internal/board"
)

//go:embed prompts/*.md prompts/roles/*.md
var reviewPromptFS embed.FS

var reviewPromptTemplates = template.Must(
	template.New("review-prompts").Option("missingkey=error").ParseFS(
		reviewPromptFS,
		"prompts/*.md",
		"prompts/roles/*.md",
	),
)

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

func renderReviewTemplate(name string, data any) string {
	var out bytes.Buffer
	if err := reviewPromptTemplates.ExecuteTemplate(&out, name, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(out.String()) + "\n"
}

func renderRoleRules(role string) string {
	name, ok := map[string]string{
		"PMQA":     "role-pmqa.md",
		"Security": "role-security.md",
		"PM":       "role-pm.md",
		"QA":       "role-qa.md",
		"CSA":      "role-csa.md",
		"Hacker":   "role-hacker.md",
	}[role]
	if !ok {
		return ""
	}
	return renderReviewTemplate(name, nil)
}

var roleRules = map[string]string{
	"PMQA":     renderRoleRules("PMQA"),
	"Security": renderRoleRules("Security"),
	"PM":       renderRoleRules("PM"),
	"QA":       renderRoleRules("QA"),
	"CSA":      renderRoleRules("CSA"),
	"Hacker":   renderRoleRules("Hacker"),
}

var (
	tierRules                 = renderReviewTemplate("tiers.md", nil)
	structuredFindingRules    = renderReviewTemplate("structured-findings.md", nil)
	lastMessageOutputContract = strings.TrimSpace(renderReviewTemplate("last-message-output.md", nil))
)

func incrementalScopeRules(ctx reviewContext) string {
	return renderReviewTemplate("scope-incremental.md", incrementalScopeData{
		Base: ctx.base, Commit: ctx.commit, Reviewed: ctx.reviewed,
	})
}

func buildReviewContract(ctx reviewContext) string {
	scope := renderReviewTemplate("scope-full.md", nil)
	if ctx.reviewed != "" {
		scope = incrementalScopeRules(ctx)
	}
	outputContract := ""
	if usesLastMessageReport(ctx.agent) {
		outputContract = lastMessageOutputContract
	}
	return renderReviewTemplate("review-contract.md", reviewContractData{
		Role:                      ctx.role,
		Scope:                     scope,
		RoleRules:                 roleRules[ctx.role],
		TierRules:                 tierRules,
		StructuredFindingRules:    structuredFindingRules,
		InspectionRules:           ctx.settings.inspectionRules,
		ReportLanguageRule:        reportLanguageRule(ctx.reportLanguage),
		LastMessageOutputContract: outputContract,
	})
}

func reportLanguageRule(language string) string {
	if language == "" {
		return ""
	}
	return renderReviewTemplate("report-language.md", struct{ Language string }{Language: language})
}

func buildPrompt(ctx reviewContext, evidenceFile, taskContext string) string {
	automatic, caller := reviewPromptContext(ctx)
	return renderReviewTemplate("bootstrap.md", reviewBootstrapData{
		Role:                   ctx.role,
		Root:                   ctx.root,
		Base:                   ctx.base,
		Commit:                 ctx.commit,
		EvidenceFile:           evidenceFile,
		ContractFile:           filepath.Join(filepath.Dir(evidenceFile), "review-contract.md"),
		TaskContext:            taskContext,
		AutomaticReviewContext: automatic,
		CallerReviewContext:    caller,
	})
}

func usesLastMessageReport(agent string) bool {
	switch agent {
	case "claude", "cursor", "grok":
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
