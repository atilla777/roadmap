package main

import (
	"bytes"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"html/template"
	iofs "io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
)

//go:embed web/templates web/static
var webFS embed.FS

var funcMap = template.FuncMap{
	"nextStatus": func(s string) string {
		switch s {
		case "pending":
			return "active"
		case "active":
			return "done"
		default:
			return "pending"
		}
	},
	"priorityLabel": func(p int) string {
		return priorityLabel(p)
	},
}

func loadTemplates() *template.Template {
	return template.Must(
		template.New("").Funcs(funcMap).ParseFS(webFS,
			"web/templates/layout.html",
			"web/templates/projects.html",
			"web/templates/project.html",
			"web/templates/partials/phase.html",
			"web/templates/partials/task.html",
		),
	)
}

type pageData struct {
	Title       string
	ProjectID   int
	ProjectName string
	Content     template.HTML
}

func renderPage(w http.ResponseWriter, tmpl *template.Template, contentTemplate string, data any, page pageData) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, contentTemplate, data); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	page.Content = template.HTML(buf.String())
	tmpl.ExecuteTemplate(w, "layout", page)
}

// pathID parses the {id} path value and returns 400 on failure.
func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return 0, false
	}
	return id, true
}

// validateWebTitle checks title is non-empty and within length limit.
func validateWebTitle(w http.ResponseWriter, title string) bool {
	if strings.TrimSpace(title) == "" {
		http.Error(w, "title required", 400)
		return false
	}
	if len(title) > maxTitleLen {
		http.Error(w, fmt.Sprintf("title too long (max %d characters)", maxTitleLen), 400)
		return false
	}
	return true
}

// validateWebDesc checks description length limit.
func validateWebDesc(w http.ResponseWriter, desc string) bool {
	if len(desc) > maxDescLen {
		http.Error(w, fmt.Sprintf("description too long (max %d characters)", maxDescLen), 400)
		return false
	}
	return true
}

func cmdServe(db *sql.DB, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "port to listen on")
	fs.Parse(args)

	tmpl := loadTemplates()
	mux := http.NewServeMux()

	// Static files
	staticFS, _ := iofs.Sub(webFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Pages
	mux.HandleFunc("GET /{$}", handleProjects(db, tmpl))
	mux.HandleFunc("GET /projects/{id}", handleProject(db, tmpl))

	// Phase API
	mux.HandleFunc("POST /projects/{id}/phases", handlePhaseCreate(db, tmpl))
	mux.HandleFunc("PUT /phases/{id}", handlePhaseUpdate(db, tmpl))
	mux.HandleFunc("DELETE /phases/{id}", handlePhaseDelete(db))
	mux.HandleFunc("PUT /phases/{id}/order", handlePhaseOrder(db))

	// Task API
	mux.HandleFunc("POST /phases/{id}/tasks", handleTaskCreateInPhase(db, tmpl))
	mux.HandleFunc("POST /projects/{id}/tasks", handleTaskCreateBacklog(db, tmpl))
	mux.HandleFunc("PUT /tasks/{id}", handleTaskUpdate(db, tmpl))
	mux.HandleFunc("DELETE /tasks/{id}", handleTaskDelete(db))
	mux.HandleFunc("PUT /tasks/{id}/order", handleTaskOrder(db))

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("roadmap web → http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// --- Page handlers ---

func handleProjects(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, name, path, description, created_at FROM projects ORDER BY name`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		projects, _ := scanProjects(rows)

		data := map[string]any{"Projects": projects}
		renderPage(w, tmpl, "projects-content", data, pageData{Title: "Projects"})
	}
}

func handleProject(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := pathID(w, r)
		if !ok {
			return
		}

		row := db.QueryRow(`SELECT id, name, path, description, created_at FROM projects WHERE id = ?`, projectID)
		proj, err := scanProject(row)
		if err != nil {
			http.Error(w, "project not found", 404)
			return
		}

		// Get phases
		phaseRows, err := db.Query(
			`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
			projectID,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		phases, _ := scanPhases(phaseRows)
		phaseRows.Close()

		// Get all tasks for this project
		taskRows, err := db.Query(
			`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? ORDER BY t.sort_order, t.id`,
			projectID,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tasks, _ := scanTasks(taskRows)
		taskRows.Close()

		// Group tasks by phase
		type phaseData struct {
			Phase Phase
			Tasks []Task
		}
		var phaseList []phaseData
		tasksByPhase := make(map[int64][]Task)
		var backlog []Task
		for _, t := range tasks {
			if t.PhaseID.Valid {
				tasksByPhase[t.PhaseID.Int64] = append(tasksByPhase[t.PhaseID.Int64], t)
			} else {
				backlog = append(backlog, t)
			}
		}
		for _, p := range phases {
			phaseList = append(phaseList, phaseData{Phase: p, Tasks: tasksByPhase[int64(p.ID)]})
		}

		data := map[string]any{
			"ProjectID":   proj.ID,
			"ProjectDesc": proj.Description,
			"Phases":      phaseList,
			"Backlog":     backlog,
		}
		renderPage(w, tmpl, "project-content", data, pageData{
			Title: proj.Name, ProjectID: proj.ID, ProjectName: proj.Name,
		})
	}
}

// --- Phase API handlers ---

func handlePhaseCreate(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := pathID(w, r)
		if !ok {
			return
		}
		title := strings.TrimSpace(r.FormValue("title"))
		if !validateWebTitle(w, title) {
			return
		}

		// Verify project exists
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = ?`, projectID).Scan(&exists); err != nil || exists == 0 {
			http.Error(w, "project not found", 404)
			return
		}

		var maxOrder int
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM phases WHERE project_id = ?`, projectID).Scan(&maxOrder)

		res, err := db.Exec(
			`INSERT INTO phases (project_id, title, sort_order) VALUES (?, ?, ?)`,
			projectID, title, maxOrder+1,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		id, _ := res.LastInsertId()

		phase := Phase{ID: int(id), ProjectID: projectID, Title: title, SortOrder: maxOrder + 1}
		data := struct {
			Phase Phase
			Tasks []Task
		}{Phase: phase}
		tmpl.ExecuteTemplate(w, "phase", data)
	}
}

func handlePhaseUpdate(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		r.ParseForm()

		title := r.FormValue("title")
		desc := r.FormValue("description")
		descSet := r.Form.Has("description")

		if title != "" {
			if len(title) > maxTitleLen {
				http.Error(w, fmt.Sprintf("title too long (max %d characters)", maxTitleLen), 400)
				return
			}
			db.Exec(`UPDATE phases SET title = ? WHERE id = ?`, title, id)
		}
		if descSet {
			if !validateWebDesc(w, desc) {
				return
			}
			db.Exec(`UPDATE phases SET description = ? WHERE id = ?`, desc, id)
		}

		// Return updated phase fragment
		row := db.QueryRow(`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE id = ?`, id)
		phase, err := scanPhase(row)
		if err != nil {
			http.Error(w, "phase not found", 404)
			return
		}

		taskRows, err := db.Query(
			`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.phase_id = ? ORDER BY t.sort_order, t.id`, id,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tasks, _ := scanTasks(taskRows)
		taskRows.Close()

		data := struct {
			Phase Phase
			Tasks []Task
		}{Phase: phase, Tasks: tasks}
		tmpl.ExecuteTemplate(w, "phase", data)
	}
}

func handlePhaseDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		db.Exec(`UPDATE tasks SET phase_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE phase_id = ?`, id)
		db.Exec(`DELETE FROM phases WHERE id = ?`, id)
		w.WriteHeader(200)
	}
}

func handlePhaseOrder(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		newPos, err := strconv.Atoi(r.FormValue("position"))
		if err != nil {
			http.Error(w, "invalid position", 400)
			return
		}

		// Get phase's project_id
		var projectID int
		err = db.QueryRow(`SELECT project_id FROM phases WHERE id = ?`, id).Scan(&projectID)
		if err != nil {
			http.Error(w, "phase not found", 404)
			return
		}

		rows, err := db.Query(
			`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
			projectID,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		phases, _ := scanPhases(rows)
		rows.Close()

		currentIdx := -1
		for i, p := range phases {
			if p.ID == id {
				currentIdx = i
				break
			}
		}
		if currentIdx == -1 {
			http.Error(w, "phase not found", 404)
			return
		}

		if newPos < 0 {
			newPos = 0
		}
		if newPos >= len(phases) {
			newPos = len(phases) - 1
		}

		phase := phases[currentIdx]
		phases = append(phases[:currentIdx], phases[currentIdx+1:]...)
		rear := make([]Phase, len(phases[newPos:]))
		copy(rear, phases[newPos:])
		phases = append(phases[:newPos], phase)
		phases = append(phases, rear...)

		for i, p := range phases {
			db.Exec(`UPDATE phases SET sort_order = ? WHERE id = ?`, i, p.ID)
		}
		w.WriteHeader(200)
	}
}

// --- Task API handlers ---

func handleTaskCreateInPhase(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		phaseID, ok := pathID(w, r)
		if !ok {
			return
		}
		title := strings.TrimSpace(r.FormValue("title"))
		if !validateWebTitle(w, title) {
			return
		}

		priority, err := validatePriority(r.FormValue("priority"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		dueDate := r.FormValue("due_date")
		if err := validateDueDate(dueDate); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// Get project_id from phase
		var projectID int
		err = db.QueryRow(`SELECT project_id FROM phases WHERE id = ?`, phaseID).Scan(&projectID)
		if err != nil {
			http.Error(w, "phase not found", 404)
			return
		}

		var maxOrder int
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE phase_id = ?`, phaseID).Scan(&maxOrder)

		res, err := db.Exec(
			`INSERT INTO tasks (project_id, title, phase_id, sort_order, priority, due_date) VALUES (?, ?, ?, ?, ?, ?)`,
			projectID, title, phaseID, maxOrder+1, priority, dueDate,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		id, _ := res.LastInsertId()

		task := Task{ID: int(id), ProjectID: projectID, Title: title, Status: "pending", PhaseID: sql.NullInt64{Int64: int64(phaseID), Valid: true}, SortOrder: maxOrder + 1, Priority: priority, DueDate: dueDate}
		tmpl.ExecuteTemplate(w, "task", task)
	}
}

func handleTaskCreateBacklog(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := pathID(w, r)
		if !ok {
			return
		}
		title := strings.TrimSpace(r.FormValue("title"))
		if !validateWebTitle(w, title) {
			return
		}

		priority, err := validatePriority(r.FormValue("priority"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		dueDate := r.FormValue("due_date")
		if err := validateDueDate(dueDate); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// Verify project exists
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = ?`, projectID).Scan(&exists); err != nil || exists == 0 {
			http.Error(w, "project not found", 404)
			return
		}

		var maxOrder int
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE project_id = ? AND phase_id IS NULL`, projectID).Scan(&maxOrder)

		res, err := db.Exec(
			`INSERT INTO tasks (project_id, title, sort_order, priority, due_date) VALUES (?, ?, ?, ?, ?)`,
			projectID, title, maxOrder+1, priority, dueDate,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		id, _ := res.LastInsertId()

		task := Task{ID: int(id), ProjectID: projectID, Title: title, Status: "pending", SortOrder: maxOrder + 1, Priority: priority, DueDate: dueDate}
		tmpl.ExecuteTemplate(w, "task", task)
	}
}

func handleTaskUpdate(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}

		r.ParseForm()
		status := r.FormValue("status")
		title := r.FormValue("title")
		desc := r.FormValue("description")
		descSet := r.Form.Has("description")
		priorityStr := r.FormValue("priority")
		prioritySet := r.Form.Has("priority")
		dueStr := r.FormValue("due_date")
		dueSet := r.Form.Has("due_date")

		if title != "" && len(title) > maxTitleLen {
			http.Error(w, fmt.Sprintf("title too long (max %d characters)", maxTitleLen), 400)
			return
		}
		if descSet && !validateWebDesc(w, desc) {
			return
		}

		if status != "" {
			db.Exec(`UPDATE tasks SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, status, id)
		}
		if title != "" {
			db.Exec(`UPDATE tasks SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, title, id)
		}
		if descSet {
			db.Exec(`UPDATE tasks SET description = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, desc, id)
		}
		if prioritySet {
			p, err := validatePriority(priorityStr)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			db.Exec(`UPDATE tasks SET priority = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, p, id)
		}
		if dueSet {
			if err := validateDueDate(dueStr); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			db.Exec(`UPDATE tasks SET due_date = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, dueStr, id)
		}

		// Return updated task fragment
		row := db.QueryRow(`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.id = ?`, id)
		task, err := scanTask(row)
		if err != nil {
			http.Error(w, "task not found", 404)
			return
		}
		tmpl.ExecuteTemplate(w, "task", task)
	}
}

func handleTaskDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
		w.WriteHeader(200)
	}
}

func handleTaskOrder(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID, ok := pathID(w, r)
		if !ok {
			return
		}
		newPos, err := strconv.Atoi(r.FormValue("position"))
		if err != nil {
			http.Error(w, "invalid position", 400)
			return
		}

		// Get task's phase
		var phaseID sql.NullInt64
		var projectID int
		err = db.QueryRow(`SELECT project_id, phase_id FROM tasks WHERE id = ?`, taskID).Scan(&projectID, &phaseID)
		if err != nil {
			http.Error(w, "task not found", 404)
			return
		}

		var rows *sql.Rows
		if phaseID.Valid {
			rows, err = db.Query(
				`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.phase_id = ? ORDER BY t.sort_order, t.id`,
				projectID, phaseID.Int64,
			)
		} else {
			rows, err = db.Query(
				`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.phase_id IS NULL ORDER BY t.sort_order, t.id`,
				projectID,
			)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tasks, _ := scanTasks(rows)
		rows.Close()

		currentIdx := -1
		for i, t := range tasks {
			if t.ID == taskID {
				currentIdx = i
				break
			}
		}
		if currentIdx == -1 {
			http.Error(w, "task not found in list", 404)
			return
		}

		if newPos < 0 {
			newPos = 0
		}
		if newPos >= len(tasks) {
			newPos = len(tasks) - 1
		}

		t := tasks[currentIdx]
		tasks = append(tasks[:currentIdx], tasks[currentIdx+1:]...)
		rear := make([]Task, len(tasks[newPos:]))
		copy(rear, tasks[newPos:])
		tasks = append(tasks[:newPos], t)
		tasks = append(tasks, rear...)

		for i, t := range tasks {
			db.Exec(`UPDATE tasks SET sort_order = ? WHERE id = ?`, i, t.ID)
		}
		w.WriteHeader(200)
	}
}
