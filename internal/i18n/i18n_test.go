package i18n

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"
)

var argumentPattern = regexp.MustCompile(`\.V([0-9]+)`)

func readCatalog(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := catalogs.ReadFile("locales/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var messages map[string]string
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	return messages
}

func arguments(message string) map[string]bool {
	out := map[string]bool{}
	for _, match := range argumentPattern.FindAllStringSubmatch(message, -1) {
		out[match[1]] = true
	}
	return out
}

// Catalog tests protect translation mechanics, not UI wording or appearance.
func TestCatalogs(t *testing.T) {
	en, cn := readCatalog(t, "en"), readCatalog(t, "zh-CN")
	if len(en) != len(cn) {
		t.Fatalf("catalog sizes differ: %d, %d", len(en), len(cn))
	}
	for id, english := range en {
		chinese, ok := cn[id]
		if !ok {
			t.Errorf("Chinese translation missing: %s", id)
			continue
		}
		if !reflect.DeepEqual(arguments(english), arguments(chinese)) {
			t.Errorf("argument mismatch: %s", id)
		}
		for lang, message := range map[string]string{"en": english, "cn": chinese} {
			if message == "" {
				t.Errorf("empty translation: %s/%s", lang, id)
			}
			if _, err := template.New(id).Parse(message); err != nil {
				t.Errorf("%s/%s: %v", lang, id, err)
			}
		}
	}
}

func TestText(t *testing.T) {
	for _, tc := range []struct {
		lang, id string
		args     []any
		want     string
	}{
		{"en", "cli.subcommand_is_not_implemented", []any{"demo"}, "subcommand is not implemented: demo"},
		{"cn", "cli.subcommand_is_not_implemented", []any{"demo"}, "子命令尚未实现: demo"},
		{"unknown", "cli.subcommand_is_not_implemented", []any{"demo"}, "子命令尚未实现: demo"},
		{"en", "config.unknown_task_scale", []any{"a\"b"}, "unknown task scale: \"a\\\"b\""},
		{"cn", "launch.prompt.resume_head", []any{"{{.V9}} <&>"}, "继续 Kanban 任务 {{.V9}} <&>."},
		{"en", "missing.id", nil, "missing.id"},
		{"en", "", nil, ""},
	} {
		if got := Text(tc.lang, tc.id, tc.args...); got != tc.want {
			t.Errorf("%s/%s = %q, want %q", tc.lang, tc.id, got, tc.want)
		}
	}
}

// Check literal call sites in every platform's source, including Windows files.
// This catches missing keys and dropped or duplicated interpolation arguments
// even for error branches that are hard to trigger in runtime tests.
func TestCatalogCallSites(t *testing.T) {
	messages := readCatalog(t, "en")
	files := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		if f.Name.Name == "i18n" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			offset := 0
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				qualifier, ok := fun.X.(*ast.Ident)
				if !ok {
					return true
				}
				if qualifier.Name != "config" && qualifier.Name != "i18n" {
					return true
				}
				name = fun.Sel.Name
				if qualifier.Name == "i18n" {
					offset = 1
				}
			}
			switch name {
			case "Text", "t", "text", "launchError", "notifyError", "probeError", "takeoverError", "windowError", "kanbanError", "configErrorf":
			case "newGate", "wrapFS", "configErrorfWrap", "parsePositiveFloat":
				offset = 1
			case "usageFail":
				if f.Name.Name == "board" || f.Name.Name == "launch" {
					offset = 1
				}
			default:
				return true
			}
			if len(call.Args) <= offset {
				return true
			}
			literal, ok := call.Args[offset].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			id, _ := strconv.Unquote(literal.Value)
			if id == "" {
				return true
			}
			message, ok := messages[id]
			if !ok {
				t.Errorf("%s: unknown message %q", files.Position(call.Pos()), id)
				return true
			}
			expected := len(arguments(message))
			actual := len(call.Args) - offset - 1
			if expected != actual {
				t.Errorf("%s: %s takes %d arguments, got %d", files.Position(call.Pos()), id, expected, actual)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
