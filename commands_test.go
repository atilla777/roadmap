package main

import (
	"testing"
)

func TestReorderArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"positional only", []string{"hello", "world"}, []string{"hello", "world"}},
		{"flags only", []string{"--name", "foo"}, []string{"--name", "foo"}},
		{"flags after positional", []string{"title", "--phase", "P1"}, []string{"--phase", "P1", "title"}},
		{"mixed", []string{"--desc", "d", "title", "--phase", "P"}, []string{"--desc", "d", "--phase", "P", "title"}},
		{"flag without value at end", []string{"title", "--verbose"}, []string{"--verbose", "title"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderArgs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("reorderArgs(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("reorderArgs(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"1", 1, false},
		{"42", 42, false},
		{"0", 0, false},
		{"abc", 0, true},
		{"", 0, true},
		{"1.5", 0, true},
	}
	for _, tt := range tests {
		got, err := parseID(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseID(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseID(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestResolvePhase_ByID(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "My Phase", 0)

	result, err := resolvePhase(db, pid, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Int64 != int64(phID) {
		t.Errorf("resolvePhase by ID = %v, want %d", result, phID)
	}
}

func TestResolvePhase_ByTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "My Phase", 0)

	result, err := resolvePhase(db, pid, "My Phase")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Int64 != int64(phID) {
		t.Errorf("resolvePhase by title = %v, want %d", result, phID)
	}
}

func TestResolvePhase_NotFound(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")

	_, err := resolvePhase(db, pid, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent phase")
	}
}

func TestValidatePriority(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"none", 0, false},
		{"0", 0, false},
		{"low", 1, false},
		{"1", 1, false},
		{"medium", 2, false},
		{"med", 2, false},
		{"2", 2, false},
		{"high", 3, false},
		{"3", 3, false},
		{"HIGH", 3, false},
		{"Low", 1, false},
		{"invalid", 0, true},
		{"4", 0, true},
		{"urgent", 0, true},
	}
	for _, tt := range tests {
		got, err := validatePriority(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("validatePriority(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("validatePriority(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestValidateDueDate(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"2025-03-01", false},
		{"2025-12-31", false},
		{"2025-3-01", true},
		{"20250301", true},
		{"03-01-2025", true},
		{"not-a-date", true},
		{"2025/03/01", true},
	}
	for _, tt := range tests {
		err := validateDueDate(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateDueDate(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
	}
}
