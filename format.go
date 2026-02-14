package main

import (
	"fmt"
	"strings"
)

func statusIcon(status string) string {
	switch status {
	case "active":
		return "*"
	case "done":
		return "x"
	default:
		return "-"
	}
}

// FormatList groups tasks by phase (using Phase entities) and formats them for display.
func FormatList(phases []Phase, tasks []Task) string {
	// Build a map of phase_id -> tasks
	phaseTaskMap := make(map[int64][]Task)
	var backlog []Task
	for _, t := range tasks {
		if t.PhaseID.Valid {
			phaseTaskMap[t.PhaseID.Int64] = append(phaseTaskMap[t.PhaseID.Int64], t)
		} else {
			backlog = append(backlog, t)
		}
	}

	var b strings.Builder
	first := true

	// Print phases in order
	for _, phase := range phases {
		phaseTasks := phaseTaskMap[int64(phase.ID)]
		if len(phaseTasks) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString(phase.Title)
		b.WriteString("\n")
		for _, t := range phaseTasks {
			fmt.Fprintf(&b, "  %s #%-3d %s  %s\n", statusIcon(t.Status), t.ID, t.Title, t.Status)
		}
	}

	// Print backlog (tasks without phase)
	if len(backlog) > 0 {
		if !first {
			b.WriteString("\n")
		}
		b.WriteString("(backlog)\n")
		for _, t := range backlog {
			fmt.Fprintf(&b, "  %s #%-3d %s  %s\n", statusIcon(t.Status), t.ID, t.Title, t.Status)
		}
	}

	return b.String()
}

// FormatContext produces a compact LLM-friendly summary.
func FormatContext(projectName string, done, active, next []Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[roadmap: %s]\n", projectName)

	if len(done) > 0 {
		b.WriteString("done: ")
		b.WriteString(compactList(done, false))
		b.WriteString("\n")
	}
	if len(active) > 0 {
		b.WriteString("active: ")
		b.WriteString(compactList(active, true))
		b.WriteString("\n")
	}
	if len(next) > 0 {
		b.WriteString("next: ")
		b.WriteString(compactList(next, false))
		b.WriteString("\n")
	}
	return b.String()
}

func compactList(tasks []Task, showPhase bool) string {
	parts := make([]string, len(tasks))
	for i, t := range tasks {
		s := fmt.Sprintf("#%d %s", t.ID, t.Title)
		if showPhase && t.PhaseTitle != "" {
			s += fmt.Sprintf(" [%s]", t.PhaseTitle)
		}
		parts[i] = s
	}
	return strings.Join(parts, " | ")
}
