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

// FormatList groups tasks by phase and formats them for display.
func FormatList(tasks []Task) string {
	phases := groupByPhase(tasks)
	order := phaseOrder(tasks)

	var b strings.Builder
	for i, phase := range order {
		if i > 0 {
			b.WriteString("\n")
		}
		label := phase
		if label == "" {
			label = "(no phase)"
		}
		b.WriteString(label)
		b.WriteString("\n")
		for _, t := range phases[phase] {
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
		if showPhase && t.Phase != "" {
			s += fmt.Sprintf(" [%s]", t.Phase)
		}
		parts[i] = s
	}
	return strings.Join(parts, " | ")
}

func groupByPhase(tasks []Task) map[string][]Task {
	m := make(map[string][]Task)
	for _, t := range tasks {
		m[t.Phase] = append(m[t.Phase], t)
	}
	return m
}

// phaseOrder returns phases in first-seen order.
func phaseOrder(tasks []Task) []string {
	seen := make(map[string]bool)
	var order []string
	for _, t := range tasks {
		if !seen[t.Phase] {
			seen[t.Phase] = true
			order = append(order, t.Phase)
		}
	}
	return order
}
