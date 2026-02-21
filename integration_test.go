package main

import (
	"strconv"
	"strings"
	"testing"
)

// --- Project commands ---

func TestCmdProjectAdd(t *testing.T) {
	db := testDB(t)
	err := cmdProjectAdd(db, []string{"myproj", "--path", "/tmp/test"})
	if err != nil {
		t.Fatal(err)
	}
	// Verify project exists
	row := db.QueryRow(`SELECT name, path FROM projects WHERE name = 'myproj'`)
	var name, path string
	if err := row.Scan(&name, &path); err != nil {
		t.Fatal(err)
	}
	if name != "myproj" || path != "/tmp/test" {
		t.Errorf("got name=%q path=%q", name, path)
	}
}

func TestCmdProjectAdd_Duplicate(t *testing.T) {
	db := testDB(t)
	if err := cmdProjectAdd(db, []string{"proj", "--path", "/a"}); err != nil {
		t.Fatal(err)
	}
	err := cmdProjectAdd(db, []string{"proj", "--path", "/b"})
	if err == nil {
		t.Error("expected error for duplicate project name")
	}
}

func TestCmdProjectAdd_NoArgs(t *testing.T) {
	db := testDB(t)
	err := cmdProjectAdd(db, nil)
	if err == nil {
		t.Error("expected error for no args")
	}
}

func TestCmdProjectList(t *testing.T) {
	db := testDB(t)
	// Empty list should not error
	if err := cmdProjectList(db); err != nil {
		t.Fatal(err)
	}

	createProject(t, db, "alpha", "/a")
	createProject(t, db, "beta", "/b")
	if err := cmdProjectList(db); err != nil {
		t.Fatal(err)
	}
}

func TestCmdProjectEdit(t *testing.T) {
	db := testDB(t)
	createProject(t, db, "proj", "/p")

	// Rename
	if err := cmdProjectEdit(db, []string{"proj", "--name", "newname"}); err != nil {
		t.Fatal(err)
	}
	row := db.QueryRow(`SELECT name FROM projects WHERE id = 1`)
	var name string
	row.Scan(&name)
	if name != "newname" {
		t.Errorf("name = %q, want %q", name, "newname")
	}

	// Edit description
	if err := cmdProjectEdit(db, []string{"newname", "--desc", "hello"}); err != nil {
		t.Fatal(err)
	}
	row = db.QueryRow(`SELECT description FROM projects WHERE id = 1`)
	var desc string
	row.Scan(&desc)
	if desc != "hello" {
		t.Errorf("desc = %q, want %q", desc, "hello")
	}
}

func TestCmdProjectEdit_NotFound(t *testing.T) {
	db := testDB(t)
	err := cmdProjectEdit(db, []string{"nonexistent", "--name", "x"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestCmdProjectRemove(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	createTask(t, db, pid, "task1", "pending", intPtr(phID))

	if err := cmdProjectRemove(db, []string{"proj"}); err != nil {
		t.Fatal(err)
	}

	// Verify cascade
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&count)
	if count != 0 {
		t.Error("projects should be empty")
	}
	db.QueryRow(`SELECT COUNT(*) FROM phases`).Scan(&count)
	if count != 0 {
		t.Error("phases should be empty")
	}
	db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count)
	if count != 0 {
		t.Error("tasks should be empty")
	}
}

func TestCmdProjectRemove_NotFound(t *testing.T) {
	db := testDB(t)
	err := cmdProjectRemove(db, []string{"nope"})
	if err == nil {
		t.Error("expected error")
	}
}

// --- Phase commands ---

func TestCmdPhaseAdd(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")

	if err := cmdPhaseAdd(db, pid, []string{"Alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdPhaseAdd(db, pid, []string{"Beta", "--desc", "beta desc"}); err != nil {
		t.Fatal(err)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM phases WHERE project_id = ?`, pid).Scan(&count)
	if count != 2 {
		t.Errorf("got %d phases, want 2", count)
	}

	// Check sort order
	var order0, order1 int
	db.QueryRow(`SELECT sort_order FROM phases WHERE title = 'Alpha'`).Scan(&order0)
	db.QueryRow(`SELECT sort_order FROM phases WHERE title = 'Beta'`).Scan(&order1)
	if order0 >= order1 {
		t.Error("Alpha should have lower sort_order than Beta")
	}
}

func TestCmdPhaseAdd_DuplicateTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	if err := cmdPhaseAdd(db, pid, []string{"Same"}); err != nil {
		t.Fatal(err)
	}
	err := cmdPhaseAdd(db, pid, []string{"Same"})
	if err == nil {
		t.Error("expected error for duplicate phase title")
	}
}

func TestCmdPhaseList(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase 1", 0)
	createTask(t, db, pid, "t1", "pending", intPtr(phID))

	if err := cmdPhaseList(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdPhaseList_Empty(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	if err := cmdPhaseList(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdPhaseEdit(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Old Title", 0)

	if err := cmdPhaseEdit(db, pid, []string{itoa(phID), "--title", "New Title"}); err != nil {
		t.Fatal(err)
	}
	var title string
	db.QueryRow(`SELECT title FROM phases WHERE id = ?`, phID).Scan(&title)
	if title != "New Title" {
		t.Errorf("title = %q, want %q", title, "New Title")
	}

	if err := cmdPhaseEdit(db, pid, []string{itoa(phID), "--desc", "new desc"}); err != nil {
		t.Fatal(err)
	}
	var desc string
	db.QueryRow(`SELECT description FROM phases WHERE id = ?`, phID).Scan(&desc)
	if desc != "new desc" {
		t.Errorf("desc = %q", desc)
	}
}

func TestCmdPhaseMove(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	ph1 := createPhase(t, db, pid, "A", 0)
	createPhase(t, db, pid, "B", 1)
	createPhase(t, db, pid, "C", 2)

	// Move A to position 2
	if err := cmdPhaseMove(db, pid, []string{itoa(ph1), "2"}); err != nil {
		t.Fatal(err)
	}
	// Check new order: B, C, A
	rows, _ := db.Query(`SELECT title FROM phases WHERE project_id = ? ORDER BY sort_order`, pid)
	var titles []string
	for rows.Next() {
		var title string
		rows.Scan(&title)
		titles = append(titles, title)
	}
	rows.Close()
	if len(titles) != 3 || titles[0] != "B" || titles[1] != "C" || titles[2] != "A" {
		t.Errorf("order = %v, want [B C A]", titles)
	}
}

func TestCmdPhaseRemove(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	taskID := createTask(t, db, pid, "task1", "pending", intPtr(phID))

	if err := cmdPhaseRemove(db, pid, []string{itoa(phID)}); err != nil {
		t.Fatal(err)
	}

	// Phase should be gone
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ?`, phID).Scan(&count)
	if count != 0 {
		t.Error("phase should be deleted")
	}

	// Task should exist but with NULL phase
	var phaseID interface{}
	db.QueryRow(`SELECT phase_id FROM tasks WHERE id = ?`, taskID).Scan(&phaseID)
	if phaseID != nil {
		t.Error("task should have NULL phase_id after phase removal")
	}
}

// --- Task commands ---

func TestCmdAdd(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")

	// Add without phase
	if err := cmdAdd(db, pid, []string{"Task 1"}); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE project_id = ?`, pid).Scan(&count)
	if count != 1 {
		t.Errorf("got %d tasks, want 1", count)
	}

	// Add with phase
	phID := createPhase(t, db, pid, "Phase 1", 0)
	if err := cmdAdd(db, pid, []string{"Task 2", "--phase", itoa(phID)}); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE phase_id = ?`, phID).Scan(&count)
	if count != 1 {
		t.Errorf("got %d tasks in phase, want 1", count)
	}

	// Add with description
	if err := cmdAdd(db, pid, []string{"Task 3", "--desc", "description"}); err != nil {
		t.Fatal(err)
	}
	var desc string
	db.QueryRow(`SELECT description FROM tasks WHERE title = 'Task 3'`).Scan(&desc)
	if desc != "description" {
		t.Errorf("desc = %q", desc)
	}
}

func TestCmdAdd_PhaseByTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createPhase(t, db, pid, "My Phase", 0)

	if err := cmdAdd(db, pid, []string{"Task", "--phase", "My Phase"}); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE phase_id IS NOT NULL AND project_id = ?`, pid).Scan(&count)
	if count != 1 {
		t.Errorf("got %d tasks, want 1", count)
	}
}

func TestCmdStart(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	if err := cmdStart(db, pid, []string{itoa(taskID)}); err != nil {
		t.Fatal(err)
	}
	var status string
	db.QueryRow(`SELECT status FROM tasks WHERE id = ?`, taskID).Scan(&status)
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}
}

func TestCmdDone(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "active", nil)

	// Done with explicit ID
	if err := cmdDone(db, pid, []string{itoa(taskID)}); err != nil {
		t.Fatal(err)
	}
	var status string
	db.QueryRow(`SELECT status FROM tasks WHERE id = ?`, taskID).Scan(&status)
	if status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestCmdDone_SingleActive(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "active", nil)

	// Done without ID (should find single active)
	if err := cmdDone(db, pid, nil); err != nil {
		t.Fatal(err)
	}
	var status string
	db.QueryRow(`SELECT status FROM tasks WHERE id = ?`, taskID).Scan(&status)
	if status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestCmdDone_NoActive(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")

	err := cmdDone(db, pid, nil)
	if err == nil {
		t.Error("expected error for no active tasks")
	}
	if !strings.Contains(err.Error(), "no active tasks") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdDone_MultipleActive(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createTask(t, db, pid, "t1", "active", nil)
	createTask(t, db, pid, "t2", "active", nil)

	err := cmdDone(db, pid, nil)
	if err == nil {
		t.Error("expected error for multiple active tasks")
	}
	if !strings.Contains(err.Error(), "multiple active tasks") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdEdit(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "old title", "pending", nil)

	// Edit title
	if err := cmdEdit(db, pid, []string{itoa(taskID), "--title", "new title"}); err != nil {
		t.Fatal(err)
	}
	var title string
	db.QueryRow(`SELECT title FROM tasks WHERE id = ?`, taskID).Scan(&title)
	if title != "new title" {
		t.Errorf("title = %q", title)
	}

	// Edit phase
	phID := createPhase(t, db, pid, "P1", 0)
	if err := cmdEdit(db, pid, []string{itoa(taskID), "--phase", itoa(phID)}); err != nil {
		t.Fatal(err)
	}
	var phaseid int
	db.QueryRow(`SELECT phase_id FROM tasks WHERE id = ?`, taskID).Scan(&phaseid)
	if phaseid != phID {
		t.Errorf("phase_id = %d, want %d", phaseid, phID)
	}

	// Edit description
	if err := cmdEdit(db, pid, []string{itoa(taskID), "--desc", "a desc"}); err != nil {
		t.Fatal(err)
	}
	var desc string
	db.QueryRow(`SELECT description FROM tasks WHERE id = ?`, taskID).Scan(&desc)
	if desc != "a desc" {
		t.Errorf("desc = %q", desc)
	}

	// Clear phase
	if err := cmdEdit(db, pid, []string{itoa(taskID), "--phase", ""}); err != nil {
		t.Fatal(err)
	}
	var nullPhase interface{}
	db.QueryRow(`SELECT phase_id FROM tasks WHERE id = ?`, taskID).Scan(&nullPhase)
	if nullPhase != nil {
		t.Error("phase_id should be NULL")
	}
}

func TestCmdMove(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	t1 := createTask(t, db, pid, "A", "pending", nil)
	createTask(t, db, pid, "B", "pending", nil)
	createTask(t, db, pid, "C", "pending", nil)

	// Move A to position 2
	if err := cmdMove(db, pid, []string{itoa(t1), "2"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.Query(`SELECT title FROM tasks WHERE project_id = ? AND phase_id IS NULL ORDER BY sort_order`, pid)
	var titles []string
	for rows.Next() {
		var title string
		rows.Scan(&title)
		titles = append(titles, title)
	}
	rows.Close()
	if len(titles) != 3 || titles[0] != "B" || titles[1] != "C" || titles[2] != "A" {
		t.Errorf("order = %v, want [B C A]", titles)
	}
}

func TestCmdRemove(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	if err := cmdRemove(db, pid, []string{itoa(taskID)}); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, taskID).Scan(&count)
	if count != 0 {
		t.Error("task should be deleted")
	}
}

func TestCmdRemove_NotFound(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")

	err := cmdRemove(db, pid, []string{"999"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestCmdCurrent(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createTask(t, db, pid, "active1", "active", nil)
	createTask(t, db, pid, "pending1", "pending", nil)

	// Should not error, displays only active
	if err := cmdCurrent(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCurrent_Empty(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	if err := cmdCurrent(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdNext(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	for i := 0; i < 7; i++ {
		createTask(t, db, pid, "task", "pending", nil)
	}
	// Should not error, shows at most 5
	if err := cmdNext(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdNext_Empty(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	if err := cmdNext(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdList(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase 1", 0)
	createTask(t, db, pid, "t1", "pending", intPtr(phID))
	createTask(t, db, pid, "t2", "active", nil)

	if err := cmdList(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdList_Empty(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	if err := cmdList(db, pid); err != nil {
		t.Fatal(err)
	}
}

func TestCmdContext(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	createTask(t, db, pid, "done1", "done", nil)
	createTask(t, db, pid, "active1", "active", nil)
	createTask(t, db, pid, "pending1", "pending", nil)

	if err := cmdContext(db, pid, "proj"); err != nil {
		t.Fatal(err)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// --- Validation tests ---

func longString(n int) string {
	return strings.Repeat("a", n)
}

func TestValidation_ProjectAdd_EmptyName(t *testing.T) {
	db := testDB(t)
	err := cmdProjectAdd(db, []string{"", "--path", "/p"})
	if err == nil {
		t.Error("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidation_ProjectAdd_LongName(t *testing.T) {
	db := testDB(t)
	err := cmdProjectAdd(db, []string{longString(256), "--path", "/p"})
	if err == nil {
		t.Error("expected error for long name")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidation_ProjectEdit_EmptyName(t *testing.T) {
	db := testDB(t)
	createProject(t, db, "proj", "/p")
	err := cmdProjectEdit(db, []string{"proj", "--name", ""})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestValidation_ProjectEdit_LongDesc(t *testing.T) {
	db := testDB(t)
	createProject(t, db, "proj", "/p")
	err := cmdProjectEdit(db, []string{"proj", "--desc", longString(10001)})
	if err == nil {
		t.Error("expected error for long description")
	}
}

func TestValidation_PhaseAdd_EmptyTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	err := cmdPhaseAdd(db, pid, []string{""})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestValidation_PhaseAdd_LongTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	err := cmdPhaseAdd(db, pid, []string{longString(256)})
	if err == nil {
		t.Error("expected error for long title")
	}
}

func TestValidation_PhaseAdd_LongDesc(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	err := cmdPhaseAdd(db, pid, []string{"phase", "--desc", longString(10001)})
	if err == nil {
		t.Error("expected error for long description")
	}
}

func TestValidation_PhaseEdit_EmptyTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	err := cmdPhaseEdit(db, pid, []string{itoa(phID), "--title", ""})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestValidation_TaskAdd_EmptyTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	err := cmdAdd(db, pid, []string{""})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestValidation_TaskAdd_LongTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	err := cmdAdd(db, pid, []string{longString(256)})
	if err == nil {
		t.Error("expected error for long title")
	}
}

func TestValidation_TaskAdd_LongDesc(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	err := cmdAdd(db, pid, []string{"task", "--desc", longString(10001)})
	if err == nil {
		t.Error("expected error for long description")
	}
}

func TestValidation_TaskEdit_LongTitle(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task", "pending", nil)
	err := cmdEdit(db, pid, []string{itoa(taskID), "--title", longString(256)})
	if err == nil {
		t.Error("expected error for long title")
	}
}

func TestValidation_TaskEdit_LongDesc(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task", "pending", nil)
	err := cmdEdit(db, pid, []string{itoa(taskID), "--desc", longString(10001)})
	if err == nil {
		t.Error("expected error for long description")
	}
}

func TestValidation_InvalidID(t *testing.T) {
	db := testDB(t)
	pid := createProject(t, db, "proj", "/p")

	if err := cmdStart(db, pid, []string{"abc"}); err == nil {
		t.Error("expected error for invalid ID")
	}
	if err := cmdRemove(db, pid, []string{"xyz"}); err == nil {
		t.Error("expected error for invalid ID")
	}
	if err := cmdPhaseRemove(db, pid, []string{"nope"}); err == nil {
		t.Error("expected error for invalid ID")
	}
}
