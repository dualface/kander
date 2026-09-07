package board

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
)

// Capture every definition, including unused and duplicate labels. Destinations
// retain their source slices; no Markdown rendering or text-wide replacement is
// involved, so labels, titles, code and surrounding whitespace remain intact.
type migrationLinkContext struct {
	parser.Context
	destinations [][]byte
}

func (c *migrationLinkContext) AddReference(r parser.Reference) {
	c.destinations = append(c.destinations, r.Destination())
	c.Context.AddReference(r)
}
func markdownURL(b []byte) string {
	return string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(b))))
}

func relocateURL(raw, from, to string, mapping map[string]string) (string, error) {
	decoded := markdownURL([]byte(raw))
	if decoded == "" || strings.HasPrefix(decoded, "#") || strings.HasPrefix(decoded, "?") || strings.HasPrefix(decoded, "/") {
		return raw, nil
	}
	u, err := url.Parse(decoded)
	if err != nil {
		if !unsupportedTargetMoves(raw, from, to, mapping, false) {
			return raw, nil
		}
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.IsAbs() || u.Host != "" {
		return raw, nil
	}
	if strings.ContainsAny(u.Path, "\\\x00") {
		if !unsupportedTargetMoves(raw, from, to, mapping, false) {
			return raw, nil
		}
		return "", fmt.Errorf("non-portable relative path")
	}
	target := filepath.Clean(filepath.Join(filepath.Dir(from), filepath.FromSlash(u.Path)))
	if next, ok := mapping[migrationPathKey(target)]; ok {
		target = next
	}
	relative, err := filepath.Rel(filepath.Dir(to), target)
	if err != nil {
		return "", err
	}
	// Preserve the entire spelling when both the base and target are unchanged.
	oldTarget := filepath.Clean(filepath.Join(filepath.Dir(to), filepath.FromSlash(u.Path)))
	if migrationPathKey(oldTarget) == migrationPathKey(target) {
		return raw, nil
	}
	newPath := (&url.URL{Path: filepath.ToSlash(relative)}).EscapedPath()
	newPath = strings.NewReplacer("(", "%28", ")", "%29", "&", "%26").Replace(newPath)
	if strings.Contains(strings.Split(newPath, "/")[0], ":") {
		newPath = "./" + newPath
	}
	if strings.HasSuffix(u.Path, "/") && !strings.HasSuffix(newPath, "/") {
		newPath += "/"
	}
	suffix := ""
	if i := strings.IndexAny(decoded, "?#"); i >= 0 {
		suffix = decoded[i:]
	}
	// Usually the raw suffix is already correct (including &amp;). Entity-encoded
	// delimiters need a fresh Markdown-safe spelling of the decoded suffix.
	if i := strings.IndexAny(raw, "?#"); i >= 0 && markdownURL([]byte(raw[i:])) == suffix {
		suffix = raw[i:]
	} else {
		suffix = strings.ReplaceAll(suffix, "&", "&amp;")
	}
	return newPath + suffix, nil
}

func relocateMarkdown(source, from, to string, mapping map[string]string) (string, error) {
	if len(mapping) == 0 {
		return source, nil
	}
	data := []byte(source)
	context := &migrationLinkContext{Context: parser.NewContext()}
	document := goldmark.DefaultParser().Parse(text.NewReader(data), parser.WithContext(context))
	var unsupported error
	checkHTML := func(raw []byte) {
		tokenizer := html.NewTokenizer(bytes.NewReader(raw))
		for {
			kind := tokenizer.Next()
			if kind == html.ErrorToken {
				if tokenizer.Err() != io.EOF {
					unsupported = tokenizer.Err()
				}
				return
			}
			if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
				continue
			}
			for _, attr := range tokenizer.Token().Attr {
				if attr.Key != "href" && attr.Key != "src" && attr.Key != "srcset" {
					continue
				}
				if attr.Key == "srcset" {
					if srcsetNeedsRelocation(attr.Val, from, to, mapping) {
						unsupported = fmt.Errorf("HTML srcset requires manual conversion to Markdown")
					}
					continue
				}
				next, err := relocateURL(attr.Val, from, to, mapping)
				if err != nil || next != attr.Val {
					unsupported = fmt.Errorf("relative HTML %s requires manual conversion to Markdown", attr.Key)
				}
			}
		}
	}
	err := ast.Walk(document, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Text:
			if node.Parent().Kind() != ast.KindCodeSpan {
				for i := node.Segment.Start; i < node.Segment.Stop && i+1 < len(data); i++ {
					if data[i] != '[' || data[i+1] != '[' || i > 0 && data[i-1] == '\\' {
						continue
					}
					tail := strings.SplitN(string(data[i+2:]), "\n", 2)[0]
					if end := strings.Index(tail, "]]"); end >= 0 && unsupportedTargetMoves(tail[:end], from, to, mapping, true) {
						unsupported = fmt.Errorf("wiki link syntax requires manual conversion to Markdown")
					}
				}
			}
		case *ast.Link:
			context.destinations = append(context.destinations, node.Destination)
		case *ast.Image:
			context.destinations = append(context.destinations, node.Destination)
		case *ast.RawHTML:
			var raw []byte
			for i := 0; i < node.Segments.Len(); i++ {
				segment := node.Segments.At(i)
				raw = append(raw, segment.Value(data)...)
			}
			checkHTML(raw)
		case *ast.HTMLBlock:
			checkHTML(node.Lines().Value(data))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	if unsupported != nil {
		return "", kanbanError("board.migration_link_unsupported", from, unsupported.Error())
	}
	type edit struct {
		start, end  int
		replacement string
	}
	changes := map[int]edit{}
	for _, destination := range context.destinations {
		if len(destination) == 0 {
			continue
		}
		next, err := relocateURL(string(destination), from, to, mapping)
		if err != nil {
			return "", kanbanError("board.migration_link_unsupported", from, err.Error())
		}
		if next == string(destination) {
			continue
		}
		// Locate by both bytes and slice identity; identical URLs in examples or
		// other nodes must never be mistaken for this parser-owned destination.
		offset := 0
		found := false
		for offset < len(data) {
			i := bytes.Index(data[offset:], destination)
			if i < 0 {
				break
			}
			offset += i
			if &data[offset] == &destination[0] {
				changes[offset] = edit{offset, offset + len(destination), next}
				found = true
				break
			}
			offset++
		}
		if !found {
			return "", kanbanError("board.migration_link_unsupported", from, "destination source span unavailable")
		}
	}
	ordered := make([]edit, 0, len(changes))
	for _, change := range changes {
		ordered = append(ordered, change)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].start < ordered[j].start })
	var out strings.Builder
	at := 0
	for _, change := range ordered {
		if change.start < at {
			return "", kanbanError("board.migration_link_unsupported", from, "overlapping destinations")
		}
		out.WriteString(source[at:change.start])
		out.WriteString(change.replacement)
		at = change.end
	}
	out.WriteString(source[at:])
	return out.String(), nil
}
