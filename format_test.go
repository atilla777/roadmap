package main

import (
	"database/sql"
	"strings"
	"testing"
)

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"pending", "-"},
		{"active", "*"},
		{"done", "x"},
		{"unknown", "-"},
		{"", "-"},
	}
	for _, tt := range tests {
		got := statusIcon(tt.status)
		if got != tt.want {
			t.Errorf("statusIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestIndentDesc(t *testing.T) {
	tests := []struct {
		name   string
		desc   string
		indent string
		want   string
	}{
		{"empty", "", "  ", ""},
		{"whitespace only", "   \n  ", "  ", ""},
		{"single line", "hello", "  ", "  hello\n"},
		{"multi line", "line1\nline2\nline3", ">> ", ">> line1\n>> line2\n>> line3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := indentDesc(tt.desc, tt.indent)
			if got != tt.want {
				t.Errorf("indentDesc(%q, %q) = %q, want %q", tt.desc, tt.indent, got, tt.want)
			}
		})
	}
}

func TestFormatList_Empty(t *testing.T) {
	got := FormatList(nil, nil)
	if got != "" {
		t.Errorf("FormatList(nil, nil) = %q, want empty", got)
	}
}

func TestFormatList_NoPhases(t *testing.T) {
	tasks := []Task{
		{ID: 1, Title: "task1", Status: "pending"},
		{ID: 2, Title: "task2", Status: "done"},
	}
	got := FormatList(nil, tasks)
	if !strings.Contains(got, "(backlog)") {
		t.Error("expected (backlog) header")
	}
	if !strings.Contains(got, "task1") || !strings.Contains(got, "task2") {
		t.Error("expected both tasks in output")
	}
}

func TestFormatList_WithPhases(t *testing.T) {
	phases := []Phase{
		{ID: 1, Title: "Phase A"},
		{ID: 2, Title: "Phase B"},
	}
	tasks := []Task{
		{ID: 1, Title: "t1", Status: "active", PhaseID: sql.NullInt64{Int64: 1, Valid: true}, PhaseTitle: "Phase A"},
		{ID: 2, Title: "t2", Status: "pending", PhaseID: sql.NullInt64{Int64: 2, Valid: true}, PhaseTitle: "Phase B"},
		{ID: 3, Title: "t3", Status: "pending"},
	}
	got := FormatList(phases, tasks)
	if !strings.Contains(got, "Phase A") {
		t.Error("expected Phase A header")
	}
	if !strings.Contains(got, "Phase B") {
		t.Error("expected Phase B header")
	}
	if !strings.Contains(got, "(backlog)") {
		t.Error("expected backlog section")
	}
	// Phase A should appear before Phase B
	idxA := strings.Index(got, "Phase A")
	idxB := strings.Index(got, "Phase B")
	if idxA >= idxB {
		t.Error("expected Phase A before Phase B")
	}
}

func TestFormatList_WithDescriptions(t *testing.T) {
	phases := []Phase{{ID: 1, Title: "P1", Description: "phase desc"}}
	tasks := []Task{
		{ID: 1, Title: "t1", Status: "pending", Description: "task desc", PhaseID: sql.NullInt64{Int64: 1, Valid: true}},
	}
	got := FormatList(phases, tasks)
	if !strings.Contains(got, "phase desc") {
		t.Error("expected phase description")
	}
	if !strings.Contains(got, "task desc") {
		t.Error("expected task description")
	}
}

func TestFormatContext(t *testing.T) {
	done := []Task{{ID: 1, Title: "done1"}}
	active := []Task{{ID: 2, Title: "active1", PhaseTitle: "P1"}}
	next := []Task{{ID: 3, Title: "next1"}}
	got := FormatContext("myproj", done, active, next)

	if !strings.Contains(got, "[roadmap: myproj]") {
		t.Error("expected project header")
	}
	if !strings.Contains(got, "done: #1 done1") {
		t.Error("expected done section")
	}
	if !strings.Contains(got, "active: #2 active1 [P1]") {
		t.Error("expected active section with phase")
	}
	if !strings.Contains(got, "next: #3 next1") {
		t.Error("expected next section")
	}
}

func TestFormatContext_Empty(t *testing.T) {
	got := FormatContext("proj", nil, nil, nil)
	if !strings.Contains(got, "[roadmap: proj]") {
		t.Error("expected project header")
	}
	if strings.Contains(got, "done:") || strings.Contains(got, "active:") || strings.Contains(got, "next:") {
		t.Error("expected no sections for empty lists")
	}
}
