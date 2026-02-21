package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func testServer(t *testing.T) (*sql.DB, *httptest.Server) {
	t.Helper()
	db := testDB(t)
	tmpl := loadTemplates()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", handleProjects(db, tmpl))
	mux.HandleFunc("GET /projects/{id}", handleProject(db, tmpl))

	mux.HandleFunc("POST /projects/{id}/phases", handlePhaseCreate(db, tmpl))
	mux.HandleFunc("PUT /phases/{id}", handlePhaseUpdate(db, tmpl))
	mux.HandleFunc("DELETE /phases/{id}", handlePhaseDelete(db))
	mux.HandleFunc("PUT /phases/{id}/order", handlePhaseOrder(db))

	mux.HandleFunc("POST /phases/{id}/tasks", handleTaskCreateInPhase(db, tmpl))
	mux.HandleFunc("POST /projects/{id}/tasks", handleTaskCreateBacklog(db, tmpl))
	mux.HandleFunc("PUT /tasks/{id}", handleTaskUpdate(db, tmpl))
	mux.HandleFunc("DELETE /tasks/{id}", handleTaskDelete(db))
	mux.HandleFunc("PUT /tasks/{id}/order", handleTaskOrder(db))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return db, srv
}

func postForm(srv *httptest.Server, path string, vals url.Values) (*http.Response, error) {
	return http.Post(srv.URL+path, "application/x-www-form-urlencoded", strings.NewReader(vals.Encode()))
}

func putForm(srv *httptest.Server, path string, vals url.Values) (*http.Response, error) {
	req, _ := http.NewRequest("PUT", srv.URL+path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return http.DefaultClient.Do(req)
}

func doDelete(srv *httptest.Server, path string) (*http.Response, error) {
	req, _ := http.NewRequest("DELETE", srv.URL+path, nil)
	return http.DefaultClient.Do(req)
}

// --- Page tests ---

func TestHandleProjects(t *testing.T) {
	_, srv := testServer(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("GET / = %d, want 200", resp.StatusCode)
	}
}

func TestHandleProject(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")

	resp, err := http.Get(srv.URL + "/projects/" + itoa(pid))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("GET /projects/%d = %d, want 200", pid, resp.StatusCode)
	}
}

func TestHandleProject_NotFound(t *testing.T) {
	_, srv := testServer(t)
	resp, err := http.Get(srv.URL + "/projects/999")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("GET /projects/999 = %d, want 404", resp.StatusCode)
	}
}

func TestHandleProject_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, err := http.Get(srv.URL + "/projects/abc")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("GET /projects/abc = %d, want 400", resp.StatusCode)
	}
}

// --- Phase API tests ---

func TestHandlePhaseCreate(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")

	resp, err := postForm(srv, "/projects/"+itoa(pid)+"/phases", url.Values{"title": {"Phase 1"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("POST phase = %d, want 200", resp.StatusCode)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM phases WHERE project_id = ?`, pid).Scan(&count)
	if count != 1 {
		t.Errorf("got %d phases, want 1", count)
	}
}

func TestHandlePhaseCreate_EmptyTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")

	resp, _ := postForm(srv, "/projects/"+itoa(pid)+"/phases", url.Values{"title": {""}})
	if resp.StatusCode != 400 {
		t.Errorf("POST phase empty title = %d, want 400", resp.StatusCode)
	}
}

func TestHandlePhaseUpdate(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Old", 0)

	resp, err := putForm(srv, "/phases/"+itoa(phID), url.Values{"title": {"New"}, "description": {"desc"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("PUT phase = %d, want 200", resp.StatusCode)
	}

	var title, desc string
	db.QueryRow(`SELECT title, description FROM phases WHERE id = ?`, phID).Scan(&title, &desc)
	if title != "New" {
		t.Errorf("title = %q", title)
	}
	if desc != "desc" {
		t.Errorf("desc = %q", desc)
	}
}

func TestHandlePhaseDelete(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	createTask(t, db, pid, "task1", "pending", intPtr(phID))

	resp, err := doDelete(srv, "/phases/"+itoa(phID))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("DELETE phase = %d, want 200", resp.StatusCode)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ?`, phID).Scan(&count)
	if count != 0 {
		t.Error("phase should be deleted")
	}

	// Task should still exist with NULL phase
	var phaseID interface{}
	db.QueryRow(`SELECT phase_id FROM tasks WHERE project_id = ?`, pid).Scan(&phaseID)
	if phaseID != nil {
		t.Error("task phase_id should be NULL")
	}
}

func TestHandlePhaseOrder(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	ph1 := createPhase(t, db, pid, "A", 0)
	createPhase(t, db, pid, "B", 1)
	createPhase(t, db, pid, "C", 2)

	resp, _ := putForm(srv, "/phases/"+itoa(ph1)+"/order", url.Values{"position": {"2"}})
	if resp.StatusCode != 200 {
		t.Errorf("PUT phase order = %d, want 200", resp.StatusCode)
	}

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

// --- Task API tests ---

func TestHandleTaskCreateInPhase(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)

	resp, err := postForm(srv, "/phases/"+itoa(phID)+"/tasks", url.Values{"title": {"Task 1"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("POST task = %d, want 200", resp.StatusCode)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE phase_id = ?`, phID).Scan(&count)
	if count != 1 {
		t.Errorf("got %d tasks, want 1", count)
	}
}

func TestHandleTaskCreateInPhase_EmptyTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)

	resp, _ := postForm(srv, "/phases/"+itoa(phID)+"/tasks", url.Values{"title": {""}})
	if resp.StatusCode != 400 {
		t.Errorf("POST task empty title = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTaskCreateBacklog(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")

	resp, _ := postForm(srv, "/projects/"+itoa(pid)+"/tasks", url.Values{"title": {"Backlog Task"}})
	if resp.StatusCode != 200 {
		t.Errorf("POST backlog task = %d, want 200", resp.StatusCode)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE project_id = ? AND phase_id IS NULL`, pid).Scan(&count)
	if count != 1 {
		t.Errorf("got %d backlog tasks, want 1", count)
	}
}

func TestHandleTaskCreateBacklog_EmptyTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")

	resp, _ := postForm(srv, "/projects/"+itoa(pid)+"/tasks", url.Values{"title": {""}})
	if resp.StatusCode != 400 {
		t.Errorf("POST backlog empty title = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTaskUpdate(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	// Update status
	resp, _ := putForm(srv, "/tasks/"+itoa(taskID), url.Values{"status": {"active"}})
	if resp.StatusCode != 200 {
		t.Errorf("PUT task status = %d, want 200", resp.StatusCode)
	}
	var status string
	db.QueryRow(`SELECT status FROM tasks WHERE id = ?`, taskID).Scan(&status)
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}

	// Update title
	resp, _ = putForm(srv, "/tasks/"+itoa(taskID), url.Values{"title": {"new title"}})
	if resp.StatusCode != 200 {
		t.Errorf("PUT task title = %d, want 200", resp.StatusCode)
	}
	var title string
	db.QueryRow(`SELECT title FROM tasks WHERE id = ?`, taskID).Scan(&title)
	if title != "new title" {
		t.Errorf("title = %q", title)
	}

	// Update description
	resp, _ = putForm(srv, "/tasks/"+itoa(taskID), url.Values{"description": {"desc text"}})
	if resp.StatusCode != 200 {
		t.Errorf("PUT task desc = %d, want 200", resp.StatusCode)
	}
	var desc string
	db.QueryRow(`SELECT description FROM tasks WHERE id = ?`, taskID).Scan(&desc)
	if desc != "desc text" {
		t.Errorf("desc = %q", desc)
	}
}

func TestHandleTaskUpdate_NotFound(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := putForm(srv, "/tasks/999", url.Values{"title": {"x"}})
	if resp.StatusCode != 404 {
		t.Errorf("PUT nonexistent task = %d, want 404", resp.StatusCode)
	}
}

func TestHandleTaskDelete(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	resp, _ := doDelete(srv, "/tasks/"+itoa(taskID))
	if resp.StatusCode != 200 {
		t.Errorf("DELETE task = %d, want 200", resp.StatusCode)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, taskID).Scan(&count)
	if count != 0 {
		t.Error("task should be deleted")
	}
}

func TestHandleTaskOrder(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	t1 := createTask(t, db, pid, "A", "pending", nil)
	createTask(t, db, pid, "B", "pending", nil)
	createTask(t, db, pid, "C", "pending", nil)

	resp, _ := putForm(srv, "/tasks/"+strconv.Itoa(t1)+"/order", url.Values{"position": {"2"}})
	if resp.StatusCode != 200 {
		t.Errorf("PUT task order = %d, want 200", resp.StatusCode)
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

func TestHandleTaskOrder_NotFound(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := putForm(srv, "/tasks/999/order", url.Values{"position": {"0"}})
	if resp.StatusCode != 404 {
		t.Errorf("PUT order nonexistent task = %d, want 404", resp.StatusCode)
	}
}

// --- Validation tests ---

func longStr(n int) string {
	return strings.Repeat("x", n)
}

func TestWebValidation_PhaseCreate_LongTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	resp, _ := postForm(srv, "/projects/"+itoa(pid)+"/phases", url.Values{"title": {longStr(256)}})
	if resp.StatusCode != 400 {
		t.Errorf("POST phase long title = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseCreate_InvalidProjectID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := postForm(srv, "/projects/abc/phases", url.Values{"title": {"test"}})
	if resp.StatusCode != 400 {
		t.Errorf("POST phase invalid project id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseUpdate_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := putForm(srv, "/phases/abc", url.Values{"title": {"test"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT phase invalid id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseUpdate_LongTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	resp, _ := putForm(srv, "/phases/"+itoa(phID), url.Values{"title": {longStr(256)}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT phase long title = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseUpdate_LongDesc(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	resp, _ := putForm(srv, "/phases/"+itoa(phID), url.Values{"description": {longStr(10001)}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT phase long desc = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseDelete_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := doDelete(srv, "/phases/abc")
	if resp.StatusCode != 400 {
		t.Errorf("DELETE phase invalid id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseOrder_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := putForm(srv, "/phases/abc/order", url.Values{"position": {"0"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT phase order invalid id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_PhaseOrder_InvalidPosition(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	resp, _ := putForm(srv, "/phases/"+itoa(phID)+"/order", url.Values{"position": {"abc"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT phase order invalid position = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskCreateInPhase_LongTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)
	resp, _ := postForm(srv, "/phases/"+itoa(phID)+"/tasks", url.Values{"title": {longStr(256)}})
	if resp.StatusCode != 400 {
		t.Errorf("POST task long title = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskCreateInPhase_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := postForm(srv, "/phases/abc/tasks", url.Values{"title": {"test"}})
	if resp.StatusCode != 400 {
		t.Errorf("POST task invalid phase id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskCreateBacklog_LongTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	resp, _ := postForm(srv, "/projects/"+itoa(pid)+"/tasks", url.Values{"title": {longStr(256)}})
	if resp.StatusCode != 400 {
		t.Errorf("POST backlog long title = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskCreateBacklog_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := postForm(srv, "/projects/abc/tasks", url.Values{"title": {"test"}})
	if resp.StatusCode != 400 {
		t.Errorf("POST backlog invalid project id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskUpdate_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := putForm(srv, "/tasks/abc", url.Values{"title": {"test"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task invalid id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskUpdate_LongTitle(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task", "pending", nil)
	resp, _ := putForm(srv, "/tasks/"+itoa(taskID), url.Values{"title": {longStr(256)}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task long title = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskUpdate_LongDesc(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task", "pending", nil)
	resp, _ := putForm(srv, "/tasks/"+itoa(taskID), url.Values{"description": {longStr(10001)}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task long desc = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskDelete_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := doDelete(srv, "/tasks/abc")
	if resp.StatusCode != 400 {
		t.Errorf("DELETE task invalid id = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskOrder_InvalidID(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := putForm(srv, "/tasks/abc/order", url.Values{"position": {"0"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task order invalid id = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTaskCreateInPhase_WithPriority(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)

	resp, err := postForm(srv, "/phases/"+itoa(phID)+"/tasks", url.Values{
		"title": {"High task"}, "priority": {"high"}, "due_date": {"2025-06-01"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("POST task with priority = %d, want 200", resp.StatusCode)
	}

	var priority int
	var dueDate string
	db.QueryRow(`SELECT priority, due_date FROM tasks WHERE title = 'High task'`).Scan(&priority, &dueDate)
	if priority != 3 {
		t.Errorf("priority = %d, want 3", priority)
	}
	if dueDate != "2025-06-01" {
		t.Errorf("due_date = %q, want 2025-06-01", dueDate)
	}
}

func TestHandleTaskCreateBacklog_WithPriority(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")

	resp, _ := postForm(srv, "/projects/"+itoa(pid)+"/tasks", url.Values{
		"title": {"Low task"}, "priority": {"low"}, "due_date": {"2025-07-01"},
	})
	if resp.StatusCode != 200 {
		t.Errorf("POST backlog with priority = %d, want 200", resp.StatusCode)
	}

	var priority int
	var dueDate string
	db.QueryRow(`SELECT priority, due_date FROM tasks WHERE title = 'Low task'`).Scan(&priority, &dueDate)
	if priority != 1 {
		t.Errorf("priority = %d, want 1", priority)
	}
	if dueDate != "2025-07-01" {
		t.Errorf("due_date = %q", dueDate)
	}
}

func TestHandleTaskUpdate_PriorityAndDue(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	resp, _ := putForm(srv, "/tasks/"+itoa(taskID), url.Values{"priority": {"medium"}, "due_date": {"2025-08-01"}})
	if resp.StatusCode != 200 {
		t.Errorf("PUT task priority/due = %d, want 200", resp.StatusCode)
	}

	var priority int
	var dueDate string
	db.QueryRow(`SELECT priority, due_date FROM tasks WHERE id = ?`, taskID).Scan(&priority, &dueDate)
	if priority != 2 {
		t.Errorf("priority = %d, want 2", priority)
	}
	if dueDate != "2025-08-01" {
		t.Errorf("due_date = %q", dueDate)
	}
}

func TestHandleTaskUpdate_InvalidPriority(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	resp, _ := putForm(srv, "/tasks/"+itoa(taskID), url.Values{"priority": {"urgent"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task invalid priority = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTaskUpdate_InvalidDueDate(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task1", "pending", nil)

	resp, _ := putForm(srv, "/tasks/"+itoa(taskID), url.Values{"due_date": {"not-a-date"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task invalid due date = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTaskCreateInPhase_InvalidPriority(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	phID := createPhase(t, db, pid, "Phase", 0)

	resp, _ := postForm(srv, "/phases/"+itoa(phID)+"/tasks", url.Values{"title": {"task"}, "priority": {"urgent"}})
	if resp.StatusCode != 400 {
		t.Errorf("POST task invalid priority = %d, want 400", resp.StatusCode)
	}
}

func TestWebValidation_TaskOrder_InvalidPosition(t *testing.T) {
	db, srv := testServer(t)
	pid := createProject(t, db, "proj", "/p")
	taskID := createTask(t, db, pid, "task", "pending", nil)
	resp, _ := putForm(srv, "/tasks/"+itoa(taskID)+"/order", url.Values{"position": {"abc"}})
	if resp.StatusCode != 400 {
		t.Errorf("PUT task order invalid position = %d, want 400", resp.StatusCode)
	}
}
