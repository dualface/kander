package review

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// schemaCommands are the evidence subcommands that print a field table.
// map-legacy, aggregate, and progress stay out of this surface.
var schemaCommands = []string{"plan", "extend-plan", "assign", "disposition", "interpret", "advance", "close"}

type schemaMeta struct {
	required bool
	values   string
	textID   string
}

type schemaRow struct {
	name     string
	typeName string
	meta     schemaMeta
}

func schemaCommand(name string) bool {
	for _, command := range schemaCommands {
		if command == name {
			return true
		}
	}
	return false
}

func schemaRoot(command string) (reflect.Type, bool) {
	switch command {
	case "plan":
		return reflect.TypeFor[board.ReviewPlan](), true
	case "extend-plan":
		return reflect.TypeFor[board.ReviewPlanExtension](), true
	case "interpret":
		return reflect.TypeFor[board.ReviewInterpretation](), true
	case "assign":
		return reflect.TypeFor[board.ReviewAssignment](), true
	case "disposition":
		return reflect.TypeFor[board.ReviewDisposition](), true
	case "advance":
		return reflect.TypeFor[board.ReviewBatchAdvance](), true
	case "close":
		return reflect.TypeFor[board.ReviewCloseRequest](), true
	default:
		return nil, false
	}
}

func printEvidenceSchema(command string) int {
	text, err := evidenceSchema(command)
	if err != nil {
		return dispositionFailure(err)
	}
	fmt.Print(text)
	return 0
}

func evidenceSchema(command string) (string, error) {
	root, ok := schemaRoot(command)
	if !ok {
		return "", newGate(2, "review.schema.usage")
	}
	docs, ok := evidenceSchemaDocs()[command]
	if !ok {
		return "", newGate(2, "review.schema.usage")
	}
	rows, err := schemaRows(root, docs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "command: %s\n", command)
	for _, row := range rows {
		description := config.Text(row.meta.textID)
		if description == "" || description == row.meta.textID {
			return "", newGate(2, "review.schema.missing", row.name)
		}
		required := "no"
		if row.meta.required {
			required = "yes"
		}
		fmt.Fprintf(&b, "\nfield: %s\nrequired: %s\ntype: %s\n", row.name, required, row.typeName)
		if row.meta.values != "" {
			fmt.Fprintf(&b, "values: %s\n", row.meta.values)
		}
		fmt.Fprintf(&b, "description: %s\n", description)
	}
	return b.String(), nil
}

func schemaRows(root reflect.Type, docs map[string]schemaMeta) ([]schemaRow, error) {
	var rows []schemaRow
	walkSchema(root, "", &rows)
	seen := map[string]bool{}
	for i := range rows {
		seen[rows[i].name] = true
		meta, ok := docs[rows[i].name]
		if !ok {
			return nil, newGate(2, "review.schema.missing", rows[i].name)
		}
		rows[i].meta = meta
	}
	for name := range docs {
		if !seen[name] {
			return nil, newGate(2, "review.schema.missing", name)
		}
	}
	return rows, nil
}

func walkSchema(t reflect.Type, prefix string, rows *[]schemaRow) {
	t = derefSchema(t)
	if t.Kind() != reflect.Struct {
		return
	}
	for i := range t.NumField() {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, ok := jsonFieldName(field)
		if !ok {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := derefSchema(field.Type)
		*rows = append(*rows, schemaRow{name: path, typeName: schemaTypeName(field.Type)})
		switch ft.Kind() {
		case reflect.Struct:
			walkSchema(ft, path, rows)
		case reflect.Slice:
			if derefSchema(ft.Elem()).Kind() == reflect.Struct {
				walkSchema(ft.Elem(), path+"[]", rows)
			}
		case reflect.Map:
			if derefSchema(ft.Elem()).Kind() == reflect.Struct {
				walkSchema(ft.Elem(), path+".<key>", rows)
			}
		}
	}
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "" || tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}

func derefSchema(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func schemaTypeName(t reflect.Type) string {
	t = derefSchema(t)
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Struct:
		return "object"
	case reflect.Slice:
		elem := derefSchema(t.Elem())
		if elem.Kind() == reflect.String {
			return "array of string"
		}
		return "array of object"
	case reflect.Map:
		elem := derefSchema(t.Elem())
		switch {
		case elem.Kind() == reflect.String:
			return "map of string to string"
		case elem.Kind() == reflect.Slice && derefSchema(elem.Elem()).Kind() == reflect.String:
			return "map of string to array of string"
		default:
			return "map of string to object"
		}
	default:
		return t.Kind().String()
	}
}
