package issue

import (
	"bytes"
	"embed"
	"strings"
	"text/template"
)

//go:embed templates/*.md.tmpl
var importTemplateFS embed.FS

var importTemplates = template.Must(
	template.New("issue-import").Option("missingkey=error").ParseFS(importTemplateFS, "templates/*.md.tmpl"),
)

type importContractTemplateData struct {
	Number    int
	Owner     string
	Name      string
	SourceKey string
	SourceURL string
	FetchedAt string
	Comments  string
	TypeNote  string
	Size      string
}

func renderImportContractTemplate(name string, data importContractTemplateData) string {
	var out bytes.Buffer
	if err := importTemplates.ExecuteTemplate(&out, name, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(out.String())
}
