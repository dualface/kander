package board

import (
	"embed"
	"strings"
	"text/template"
)

//go:embed templates/*.md.tmpl
var templateFS embed.FS

// contractTemplates renders the card skeleton. The token names come from the
// schema constants rather than from literals in the Markdown, so a renamed token
// cannot leave the template and the gates that validate it out of step.
var contractTemplates = template.Must(
	template.ParseFS(templateFS, "templates/*.md.tmpl"),
)

// templateFields and templateSections give the templates short handles for the
// schema constants.
type templateFields struct {
	Type, TaskGroup, CreatedAt, Owner, Session, Window string
	StartedAt, FinishedAt, TaskBranch, Result          string
}

type templateSections struct {
	Goal, UserDecisions, ExpectedOutcome, AcceptanceCriteria string
	ThreatModel, OutOfScope, Discussion                      string
	Implementation, Summary                                  string
}

type templateData struct {
	Title       string
	Type        string
	Created     string
	Placeholder string
	F           templateFields
	S           templateSections
}

func newTemplateData(title, taskType, created string) templateData {
	return templateData{
		Title:       title,
		Type:        taskType,
		Created:     created,
		Placeholder: Placeholder,
		F: templateFields{
			Type: FieldType, TaskGroup: FieldTaskGroup, CreatedAt: FieldCreatedAt,
			Owner: FieldOwner, Session: FieldSession, Window: FieldWindow,
			StartedAt: FieldStartedAt, FinishedAt: FieldFinishedAt,
			TaskBranch: FieldTaskBranch, Result: FieldResult,
		},
		S: templateSections{
			Goal: SectionGoal, UserDecisions: SectionUserDecisions,
			ExpectedOutcome: SectionExpectedOutcome, AcceptanceCriteria: SectionAcceptanceCriteria,
			ThreatModel: SectionThreatModel, OutOfScope: SectionOutOfScope,
			Discussion: SectionDiscussion, Implementation: SectionImplementation,
			Summary: SectionSummary,
		},
	}
}

func renderTemplate(name string, data templateData) string {
	var out strings.Builder
	// The templates are embedded and parsed at init, so execution can only fail
	// on a programming error, which Must-style panicking surfaces immediately.
	if err := contractTemplates.ExecuteTemplate(&out, name, data); err != nil {
		panic(err)
	}
	return out.String()
}

func renderContract(title, taskType string) string {
	return renderTemplate("contract.md.tmpl", newTemplateData(title, typeNames[taskType], nowStamp()))
}

func smallTaskExtra() string {
	return renderTemplate("small_extra.md.tmpl", newTemplateData("", "", ""))
}
