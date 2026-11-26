package review

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

const (
	artifactRequestLimit   = 32 << 20 // Mirrors internal/server maxBody.
	artifactJSONReserve    = 64 << 10
	evidenceHardLimit      = (artifactRequestLimit - artifactJSONReserve) / 4 * 3
	evidenceTotalLimit     = 16 << 20
	evidenceFileLimit      = 1 << 20
	evidencePreviewLine    = 2 << 10
	evidencePreviewContext = 256
	evidenceUnitPreview    = 64 << 10
)

const omittedDiffsHeading = "\n=== OMITTED DIFFS ===\n" +
	"These files remain in scope. The review worktree contains the target commit. " +
	"Use the listed commands or inspect the files there. Start with --stat and inspect only relevant hunks " +
	"or truncate long lines. Do not print an entire omitted diff.\n"

type evidenceLimits struct {
	file, total, hard int
}

type evidenceSection struct {
	name, text string
	patch      *evidencePatch
}

type evidencePatch struct {
	from, to string
	prefix   string
	units    []evidenceDiff
}

type evidenceDiff struct {
	patch       *evidencePatch
	section     string
	blocks      []string
	fullBytes   int
	path        string
	parsed      bool
	fallbackOld string
	metadata    *evidenceMetadata
	unavailable string

	// An empty replacement means the original blocks are still in use.
	reason      string
	replacement diffRepresentation
}

type diffRepresentation struct {
	body, item string
}

func (r diffRepresentation) size() int { return len(r.body) + len(r.item) }

func (d *evidenceDiff) size() int {
	if d.replacement.body != "" {
		return d.replacement.size()
	}
	return d.fullBytes
}

type evidenceResult struct {
	omitted int
	largest string
}

func evidenceOmissionNotice(count int) string {
	return fmt.Sprintf("Diffs omitted or previewed for %d unit(s). See === OMITTED DIFFS ===.\n", count)
}

func evidenceOmissionOverhead(count int) int {
	if count == 0 {
		return 0
	}
	return len(evidenceOmissionNotice(count)) + len(omittedDiffsHeading)
}

func renderEvidence(intro string, sections []evidenceSection, limits evidenceLimits) (string, evidenceResult) {
	fixed := len(intro)
	var units []*evidenceDiff
	for _, section := range sections {
		fixed += len(evidenceHeading(section.name))
		if section.patch == nil {
			fixed += len(section.text)
			continue
		}
		fixed += len(section.patch.prefix)
		for i := range section.patch.units {
			units = append(units, &section.patch.units[i])
		}
	}
	count := limitEvidence(units, fixed, limits)

	var out, index strings.Builder
	out.WriteString(intro)
	if count != 0 {
		out.WriteString(evidenceOmissionNotice(count))
		index.WriteString(omittedDiffsHeading)
	}
	result := evidenceResult{omitted: count}
	largest := 0
	record := func(name string, size int) {
		if size > largest {
			largest = size
			result.largest = fmt.Sprintf("%s (%d bytes)", name, size)
		}
	}
	for _, section := range sections {
		out.WriteString(evidenceHeading(section.name))
		if section.patch == nil {
			out.WriteString(section.text)
			record(section.name, len(section.text))
			continue
		}
		out.WriteString(section.patch.prefix)
		record(section.name, len(section.patch.prefix))
		for _, unit := range section.patch.units {
			if unit.replacement.body == "" {
				for _, block := range unit.blocks {
					out.WriteString(block)
				}
			} else {
				out.WriteString(unit.replacement.body)
				index.WriteString(unit.replacement.item)
			}
			if unit.size() > largest {
				record(section.name+" "+evidenceJSONString(unit.path), unit.size())
			}
		}
	}
	out.WriteString(index.String())
	return out.String(), result
}

func evidenceHeading(name string) string { return "\n=== " + name + " ===\n" }

func limitEvidence(units []*evidenceDiff, fixed int, limits evidenceLimits) int {
	total, count := fixed, 0
	for _, unit := range units {
		if unit.fullBytes > limits.file {
			unit.reason = "per-file limit"
			if candidate := unit.smallerRepresentation(); candidate.body != "" {
				unit.replacement = candidate
				count++
			}
		}
		total += unit.size()
	}
	within := func(target int) bool { return total+evidenceOmissionOverhead(count) <= target }
	if within(limits.total) {
		return count
	}

	// Compute the minimum including the index cost, without allocating previews
	// for units that may never need them.
	minimum, minCount := fixed, 0
	for _, unit := range units {
		reason := cmp.Or(unit.reason, "total budget")
		header := unit.omittedRepresentation(reason, "")
		if header.size() < unit.fullBytes {
			minimum += header.size()
			minCount++
		} else {
			minimum += unit.fullBytes
		}
	}
	minimum += evidenceOmissionOverhead(minCount)
	target := limits.total
	switch {
	case minimum > limits.hard:
		return count
	case minimum > limits.total:
		target = limits.hard
	}
	if within(target) {
		return count
	}

	// Stable ties preserve patch order for indistinguishable fallback paths.
	slices.SortStableFunc(units, func(left, right *evidenceDiff) int {
		if order := cmp.Compare(right.fullBytes, left.fullBytes); order != 0 {
			return order
		}
		if left.section != right.section {
			if left.section == "PATCH" {
				return -1
			}
			return 1
		}
		return strings.Compare(left.path, right.path)
	})
	for _, unit := range units {
		if unit.replacement.body != "" {
			continue
		}
		unit.reason = cmp.Or(unit.reason, "total budget")
		if candidate := unit.smallerRepresentation(); candidate.body != "" {
			unit.replacement = candidate
			total += candidate.size() - unit.fullBytes
			count++
			if within(target) {
				return count
			}
		}
	}
	for _, unit := range units {
		if unit.replacement.body == "" {
			continue
		}
		header := unit.omittedRepresentation(unit.reason, "")
		if header.size() < unit.size() {
			total += header.size() - unit.size()
			unit.replacement = header
			if within(target) {
				break
			}
		}
	}
	return count
}

func (d *evidenceDiff) smallerRepresentation() diffRepresentation {
	header := d.omittedRepresentation(d.reason, "")
	if header.size() >= d.fullBytes {
		return diffRepresentation{}
	}
	if d.metadata != nil {
		if preview := previewEvidenceDiff(d.blocks); len(preview) >= len("header-only")-len("preview") {
			candidate := d.omittedRepresentation(d.reason, preview)
			if candidate.size() < d.fullBytes {
				return candidate
			}
		}
	}
	return header
}
