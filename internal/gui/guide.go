package gui

import "strings"

// StepGuide is a step's written guidance for the instructor (R-SCN-7), as Markdown.
// The frontend renders it. Go sends text, never HTML.
type StepGuide struct {
	// Briefing is what the instructor tells the trainee.
	Briefing string
	// Hints are nudges the instructor can give. They stay collapsed until asked for.
	Hints string
	// Solution is how to fix it. It stays collapsed until asked for.
	Solution string
	// References link to documentation.
	References string
}

// parseGuide splits a step's Markdown guide into its sections by its second-level
// headings: Briefing, Hints, Solution and References (or Docs, or Links), in any
// case. Text before the first heading is the briefing. A section with any other
// heading is kept in the briefing, heading and all, so nothing the author wrote is
// lost. A heading inside a code fence is text, not a heading.
func parseGuide(md string) StepGuide {
	sections := map[string]*strings.Builder{}
	var current *strings.Builder
	section := func(name string) *strings.Builder {
		if sections[name] == nil {
			sections[name] = &strings.Builder{}
		}
		return sections[name]
	}
	current = section("briefing")

	fenced := false
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			switch name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## "))); name {
			case "briefing", "hints", "solution":
				current = section(name)
				continue
			case "references", "docs", "links":
				current = section("references")
				continue
			}
			current = section("briefing") // an unknown heading stays where it is, in the briefing
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	text := func(name string) string {
		if b := sections[name]; b != nil {
			return strings.TrimSpace(b.String())
		}
		return ""
	}
	return StepGuide{Briefing: text("briefing"), Hints: text("hints"), Solution: text("solution"), References: text("references")}
}
