package review

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (d *evidenceDiff) omittedRepresentation(reason, preview string) diffRepresentation {
	var body, item strings.Builder
	for _, block := range d.blocks {
		header, _, _ := strings.Cut(block, "\n")
		body.WriteString(header)
		body.WriteByte('\n')
	}
	fmt.Fprintf(&body, "[%d diff bytes omitted from the full patch: %s. See OMITTED DIFFS.]\n", d.fullBytes, reason)
	body.WriteString(preview)

	shown := "header-only"
	if preview != "" {
		shown = "preview"
	}
	fmt.Fprintf(&item, "- section: %s\n", d.section)
	writeEvidencePath(&item, "path", d.path)
	if d.metadata != nil {
		if d.metadata.oldPath != "" {
			writeEvidencePath(&item, "old_path", d.metadata.oldPath)
		}
		fmt.Fprintf(&item, "  status: %s\n  numstat: %s\n  blob_before: %s\n  blob_after: %s\n",
			d.metadata.status, d.metadata.numstat, d.metadata.before, d.metadata.after)
	} else {
		if !d.parsed {
			item.WriteString("  path_parsed: false\n")
		}
		fmt.Fprintf(&item, "  metadata: unavailable\n  metadata_reason: %s\n", d.unavailable)
	}
	fmt.Fprintf(&item, "  diff_bytes: %d\n  shown: %s\n  reason: %s\n", d.fullBytes, shown, reason)
	d.writeEvidenceCommand(&item)
	return diffRepresentation{body: body.String(), item: item.String()}
}

func writeEvidencePath(out *strings.Builder, field, path string) {
	fmt.Fprintf(out, "  %s: %s\n", field, evidenceJSONString(path))
	if !utf8.ValidString(path) {
		fmt.Fprintf(out, "  %s_base64: %s\n", field, base64.StdEncoding.EncodeToString([]byte(path)))
	}
}

func evidenceJSONString(value string) string {
	var text strings.Builder
	encoder := json.NewEncoder(&text)
	encoder.SetEscapeHTML(false)
	// A string written to a strings.Builder cannot fail JSON encoding.
	_ = encoder.Encode(value)
	return strings.TrimSuffix(text.String(), "\n")
}

func (d *evidenceDiff) writeEvidenceCommand(out *strings.Builder) {
	old := d.fallbackOld
	if d.metadata != nil {
		old = d.metadata.oldPath
	}
	paths := []string{d.path}
	if old != "" {
		paths = []string{old, d.path}
	}
	control := strings.ContainsFunc(d.path, unicode.IsControl) || strings.ContainsFunc(old, unicode.IsControl)
	switch {
	case control && d.metadata != nil:
		d.writeEvidenceBlobCommand(out)
	case control || !d.parsed:
		fmt.Fprintf(out, "  command: unavailable\n  stat_command: %s\n",
			evidenceShellCommand(evidenceDiffArgs("--stat", d.patch.from, d.patch.to)))
	default:
		args := evidenceDiffArgs(d.patch.from, d.patch.to, "--")
		for _, path := range paths {
			if strings.HasPrefix(path, ":") || strings.ContainsAny(path, "*?[\\") {
				args = append([]string{"--literal-pathspecs"}, args...)
				break
			}
		}
		fmt.Fprintf(out, "  command: %s\n", evidenceShellCommand(append(args, paths...)))
	}
}

func (d *evidenceDiff) writeEvidenceBlobCommand(out *strings.Builder) {
	before, after := d.metadata.before, d.metadata.after
	// A zero object ID in raw metadata records absence, not an empty blob.
	// Single-sided changes can still be inspected without using the path.
	switch {
	case strings.Trim(before, "0") == "":
		fmt.Fprintf(out, "  command: git cat-file blob %s\n  command_note: added file with a control character in its path; there is no preimage blob.\n", after)
	case strings.Trim(after, "0") == "":
		fmt.Fprintf(out, "  command: git cat-file blob %s\n  command_note: deleted file with a control character in its path; there is no postimage blob.\n", before)
	default:
		fmt.Fprintf(out, "  command: git diff %s %s\n  command_note: control character in path, so compare blob IDs rather than the patch path.\n", before, after)
	}
}

func evidenceShellCommand(args []string) string {
	var command strings.Builder
	command.WriteString("git")
	literalPaths := false
	for _, arg := range args {
		command.WriteByte(' ')
		if arg != "" && !strings.ContainsFunc(arg, func(r rune) bool {
			// The Git options and config assignment may contain equal signs.
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
				strings.ContainsRune("._/-", r) || r == '=' && !literalPaths)
		}) {
			command.WriteString(arg)
		} else {
			command.WriteByte('\'')
			command.WriteString(strings.ReplaceAll(arg, "'", "'\\''"))
			command.WriteByte('\'')
		}
		literalPaths = literalPaths || arg == "--"
	}
	return command.String()
}
