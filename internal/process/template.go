package process

import (
	"fmt"
	"strings"
	"unicode"
)

// TemplateKind selects control-character rules for a template.
type TemplateKind int

const (
	// TemplateArgv rejects every Unicode control character, including TAB and newline.
	TemplateArgv TemplateKind = iota
	// TemplateArgvTab is an argv element that may contain TAB (terminal -F format strings).
	TemplateArgvTab
	// TemplateText allows TAB, LF, and CR. Other controls, including NUL, are rejected.
	// Newlines are ordinary content in Markdown and YAML front matter.
	TemplateText
)

// ValidateTemplate checks placeholders, {{ / }} escapes, and control characters.
// names is the caller whitelist. An unknown {name} is rejected and the name is in the error.
func ValidateTemplate(template string, names []string, kind TemplateKind) error {
	if err := checkTemplateControls(template, kind); err != nil {
		return err
	}
	_, err := expandTemplate(template, nil, names, false)
	return err
}

// ValidateArgv rejects empty elements, then validates each argv template element.
func ValidateArgv(elements []string, names []string, kind TemplateKind) error {
	if kind == TemplateText {
		return specErrorf("argv", "text", "argv templates cannot use the text control-character rule")
	}
	for i, element := range elements {
		if element == "" {
			return specErrorf("argv", "", "element %d is empty", i)
		}
		if err := ValidateTemplate(element, names, kind); err != nil {
			return specErrorf("argv", element, "element %d: %s", i, err.Error())
		}
	}
	return nil
}

// ExpandTemplate replaces whitelist placeholders. Substitution is not recursive.
// Runtime values are not scanned for braces. A name present in the whitelist but
// missing from values becomes the empty string.
func ExpandTemplate(template string, values map[string]string, names []string) (string, error) {
	return expandTemplate(template, values, names, true)
}

func expandTemplate(template string, values map[string]string, names []string, replace bool) (string, error) {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			return "", specErrorf("placeholder", "", "whitelist contains an empty name")
		}
		allowed[name] = struct{}{}
	}
	var out strings.Builder
	out.Grow(len(template))
	for i := 0; i < len(template); {
		switch template[i] {
		case '{':
			if i+1 < len(template) && template[i+1] == '{' {
				out.WriteByte('{')
				i += 2
				continue
			}
			end := strings.IndexByte(template[i+1:], '}')
			if end < 0 {
				return "", specErrorf("placeholder", template[i:], "unclosed {")
			}
			end += i + 1
			name := template[i+1 : end]
			if _, ok := allowed[name]; !ok {
				return "", specErrorf("placeholder", name, "unknown placeholder %s", name)
			}
			if replace {
				out.WriteString(values[name])
			}
			i = end + 1
		case '}':
			if i+1 < len(template) && template[i+1] == '}' {
				out.WriteByte('}')
				i += 2
				continue
			}
			return "", specErrorf("placeholder", "}", "unescaped }")
		default:
			out.WriteByte(template[i])
			i++
		}
	}
	if !replace {
		return "", nil
	}
	return out.String(), nil
}

// ExpandArgvOmitEmpty expands each argv element, then drops an element whose
// placeholder value is empty together with its immediately preceding standalone flag.
func ExpandArgvOmitEmpty(template []string, values map[string]string, names []string) ([]string, error) {
	if err := ValidateArgv(template, names, TemplateArgv); err != nil {
		return nil, err
	}
	var out []string
	for i, arg := range template {
		omit, err := argvElementOmits(arg, values, names)
		if err != nil {
			return nil, err
		}
		if omit {
			if i > 0 && strings.HasPrefix(template[i-1], "-") && !strings.ContainsAny(template[i-1], "{}=") && len(out) > 0 && out[len(out)-1] == template[i-1] {
				out = out[:len(out)-1]
			}
			continue
		}
		expanded, err := ExpandTemplate(arg, values, names)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded)
	}
	return out, nil
}

func argvElementOmits(arg string, values map[string]string, names []string) (bool, error) {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	for i := 0; i < len(arg); {
		switch arg[i] {
		case '{':
			if i+1 < len(arg) && arg[i+1] == '{' {
				i += 2
				continue
			}
			end := strings.IndexByte(arg[i+1:], '}')
			if end < 0 {
				return false, specErrorf("placeholder", arg[i:], "unclosed {")
			}
			end += i + 1
			name := arg[i+1 : end]
			if _, ok := allowed[name]; !ok {
				return false, specErrorf("placeholder", name, "unknown placeholder %s", name)
			}
			if values[name] == "" {
				return true, nil
			}
			i = end + 1
		case '}':
			if i+1 < len(arg) && arg[i+1] == '}' {
				i += 2
				continue
			}
			return false, specErrorf("placeholder", "}", "unescaped }")
		default:
			i++
		}
	}
	return false, nil
}

func checkTemplateControls(template string, kind TemplateKind) error {
	for _, r := range template {
		if !unicode.IsControl(r) {
			continue
		}
		switch kind {
		case TemplateArgvTab:
			if r == '\t' {
				continue
			}
		case TemplateText:
			if r == '\t' || r == '\n' || r == '\r' {
				continue
			}
		}
		return specErrorf("template", fmt.Sprintf("%q", r), "contains control character U+%04X", r)
	}
	return nil
}
