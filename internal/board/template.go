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
	Type, Size, TaskGroup, Language, CreatedAt, Owner, Session, Window string
	StartedAt, FinishedAt, TaskBranch, Result                          string
}

type templateSections struct {
	Goal, UserDecisions, ExpectedOutcome, AcceptanceCriteria string
	ThreatModel, OutOfScope, Discussion                      string
	Implementation, Summary                                  string
}

type templateBodies struct {
	Goal, UserDecisions, ExpectedOutcome, AcceptanceCriteria string
	ThreatModel, OutOfScope, Discussion                      string
}

type templateData struct {
	Title       string
	Type        string
	Size        string
	Language    string
	Created     string
	Placeholder string
	F           templateFields
	S           templateSections
	Bodies      templateBodies
}

func newTemplateData(title, taskType, language, created string) templateData {
	return templateData{
		Title:       title,
		Type:        taskType,
		Language:    language,
		Created:     created,
		Placeholder: Placeholder,
		F: templateFields{
			Type: FieldType, Size: FieldSize, TaskGroup: FieldTaskGroup, Language: FieldLanguage, CreatedAt: FieldCreatedAt,
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

// renderContract renders the card skeleton; language is the agent communication language recorded in the LANGUAGE field.
func renderContract(title, taskType, language string) string {
	return renderTemplate("contract.md.tmpl", newTemplateData(title, typeNames[taskType], language, nowStamp()))
}

func smallTaskExtra() string {
	return renderTemplate("small_extra.md.tmpl", newTemplateData("", "", "", ""))
}

// renderImportedContract renders the card of an imported source. The title and
// the section bodies are the only caller input; the skeleton, metadata fields
// and section headings always come from this template.
func renderImportedContract(request ImportRequest, size string) string {
	data := newTemplateData(request.Title, typeNames[request.Kind], request.Language, nowStamp())
	data.Size = size
	data.Bodies = templateBodies{
		Goal:               request.Contract.Goal,
		UserDecisions:      request.Contract.UserDecisions,
		ExpectedOutcome:    request.Contract.ExpectedOutcome,
		AcceptanceCriteria: request.Contract.AcceptanceCriteria,
		ThreatModel:        request.Contract.ThreatModel,
		OutOfScope:         request.Contract.OutOfScope,
		Discussion:         request.Contract.Discussion,
	}
	template := renderTemplate("import.md.tmpl", data)
	// The contract always ends on a blank line, like the skeleton template, so
	// the optional small-task sections start after a separating empty line.
	text := strings.TrimRight(template, "\n") + "\n\n"
	if size == "small" {
		text += smallTaskExtra()
	}
	return text
}
