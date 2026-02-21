package main

import (
	"database/sql"
	"testing"
)

func TestScanProject(t *testing.T) {
	db := testDB(t)
	createProject(t, db, "test-proj", "/tmp/test")

	row := db.QueryRow(`SELECT id, name, path, description, created_at FROM projects WHERE name = 'test-proj'`)
	p, err := scanProject(row)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "test-proj" {
		t.Errorf("Name = %q, want %q", p.Name, "test-proj")
	}
	if p.Path != "/tmp/test" {
		t.Errorf("Path = %q, want %q", p.Path, "/tmp/test")
	}
	if p.ID == 0 {
		t.Error("ID should be non-zero")
	}
	if p.CreatedAt == "" {
		t.Error("CreatedAt should not be empty")
	}
}

func TestScanProjects(t *testing.T) {
	db := testDB(t)
	createProject(t, db, "a", "/a")
	createProject(t, db, "b", "/b")

	rows, err := db.Query(`SELECT id, name, path, description, created_at FROM projects ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	projects, err := scanProjects(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
	if projects[0].Name != "a" || projects[1].Name != "b" {
		t.Errorf("projects = %v, want a, b", projects)
	}
}

func TestScanPhase(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createPhase(t, db, pid, "Phase 1", 0)

	row := db.QueryRow(`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ?`, pid)
	ph, err := scanPhase(row)
	if err != nil {
		t.Fatal(err)
	}
	if ph.Title != "Phase 1" {
		t.Errorf("Title = %q, want %q", ph.Title, "Phase 1")
	}
	if ph.ProjectID != pid {
		t.Errorf("ProjectID = %d, want %d", ph.ProjectID, pid)
	}
	if ph.SortOrder != 0 {
		t.Errorf("SortOrder = %d, want 0", ph.SortOrder)
	}
}

func TestScanPhases(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createPhase(t, db, pid, "A", 0)
	createPhase(t, db, pid, "B", 1)

	rows, err := db.Query(`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	phases, err := scanPhases(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) != 2 {
		t.Fatalf("got %d phases, want 2", len(phases))
	}
	if phases[0].Title != "A" || phases[1].Title != "B" {
		t.Error("unexpected phase order")
	}
}

func TestScanTask(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase 1", 0)
	createTask(t, db, pid, "task1", "active", intPtr(phID))

	row := db.QueryRow(`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ?`, pid)
	task, err := scanTask(row)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "task1" {
		t.Errorf("Title = %q, want %q", task.Title, "task1")
	}
	if task.Status != "active" {
		t.Errorf("Status = %q, want %q", task.Status, "active")
	}
	if !task.PhaseID.Valid || task.PhaseID.Int64 != int64(phID) {
		t.Errorf("PhaseID = %v, want %d", task.PhaseID, phID)
	}
	if task.PhaseTitle != "Phase 1" {
		t.Errorf("PhaseTitle = %q, want %q", task.PhaseTitle, "Phase 1")
	}
}

func TestScanTask_NoPhase(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createTask(t, db, pid, "backlog task", "pending", nil)

	row := db.QueryRow(`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ?`, pid)
	task, err := scanTask(row)
	if err != nil {
		t.Fatal(err)
	}
	if task.PhaseID.Valid {
		t.Error("expected PhaseID to be NULL")
	}
	if task.PhaseTitle != "" {
		t.Errorf("PhaseTitle = %q, want empty", task.PhaseTitle)
	}
}

func TestScanTasks(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createTask(t, db, pid, "t1", "pending", nil)
	createTask(t, db, pid, "t2", "active", nil)

	rows, err := db.Query(`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? ORDER BY t.id`, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].Title != "t1" || tasks[1].Title != "t2" {
		t.Error("unexpected task order")
	}
}

func TestScanProject_NotFound(t *testing.T) {
	db := testDB(t)
	row := db.QueryRow(`SELECT id, name, path, description, created_at FROM projects WHERE name = 'nonexistent'`)
	_, err := scanProject(row)
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}
