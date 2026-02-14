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
		rows, err := db.Query(`SELECT id, name, path, created_at FROM projects ORDER BY name`)
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
		projectID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid project id", 400)
			return
		}

		row := db.QueryRow(`SELECT id, name, path, created_at FROM projects WHERE id = ?`, projectID)
		proj, err := scanProject(row)
		if err != nil {
			http.Error(w, "project not found", 404)
			return
		}

		// Get phases
		phaseRows, err := db.Query(
			`SELECT id, project_id, title, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
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
			"ProjectID": proj.ID,
			"Phases":    phaseList,
			"Backlog":   backlog,
		}
		renderPage(w, tmpl, "project-content", data, pageData{
			Title: proj.Name, ProjectID: proj.ID, ProjectName: proj.Name,
		})
	}
}

// --- Phase API handlers ---

func handlePhaseCreate(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, _ := strconv.Atoi(r.PathValue("id"))
		title := strings.TrimSpace(r.FormValue("title"))
		if title == "" {
			http.Error(w, "title required", 400)
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

func handlePhaseDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		db.Exec(`UPDATE tasks SET phase_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE phase_id = ?`, id)
		db.Exec(`DELETE FROM phases WHERE id = ?`, id)
		w.WriteHeader(200)
	}
}

func handlePhaseOrder(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		newPos, _ := strconv.Atoi(r.FormValue("position"))

		// Get phase's project_id
		var projectID int
		err := db.QueryRow(`SELECT project_id FROM phases WHERE id = ?`, id).Scan(&projectID)
		if err != nil {
			http.Error(w, "phase not found", 404)
			return
		}

		rows, err := db.Query(
			`SELECT id, project_id, title, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
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
		phaseID, _ := strconv.Atoi(r.PathValue("id"))
		title := strings.TrimSpace(r.FormValue("title"))
		if title == "" {
			http.Error(w, "title required", 400)
			return
		}

		// Get project_id from phase
		var projectID int
		err := db.QueryRow(`SELECT project_id FROM phases WHERE id = ?`, phaseID).Scan(&projectID)
		if err != nil {
			http.Error(w, "phase not found", 404)
			return
		}

		var maxOrder int
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE phase_id = ?`, phaseID).Scan(&maxOrder)

		res, err := db.Exec(
			`INSERT INTO tasks (project_id, title, phase_id, sort_order) VALUES (?, ?, ?, ?)`,
			projectID, title, phaseID, maxOrder+1,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		id, _ := res.LastInsertId()

		task := Task{ID: int(id), ProjectID: projectID, Title: title, Status: "pending", PhaseID: sql.NullInt64{Int64: int64(phaseID), Valid: true}, SortOrder: maxOrder + 1}
		tmpl.ExecuteTemplate(w, "task", task)
	}
}

func handleTaskCreateBacklog(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, _ := strconv.Atoi(r.PathValue("id"))
		title := strings.TrimSpace(r.FormValue("title"))
		if title == "" {
			http.Error(w, "title required", 400)
			return
		}

		var maxOrder int
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE project_id = ? AND phase_id IS NULL`, projectID).Scan(&maxOrder)

		res, err := db.Exec(
			`INSERT INTO tasks (project_id, title, sort_order) VALUES (?, ?, ?)`,
			projectID, title, maxOrder+1,
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		id, _ := res.LastInsertId()

		task := Task{ID: int(id), ProjectID: projectID, Title: title, Status: "pending", SortOrder: maxOrder + 1}
		tmpl.ExecuteTemplate(w, "task", task)
	}
}

func handleTaskUpdate(db *sql.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))

		status := r.FormValue("status")
		title := r.FormValue("title")

		if status != "" {
			db.Exec(`UPDATE tasks SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, status, id)
		}
		if title != "" {
			db.Exec(`UPDATE tasks SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ?`, title, id)
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
		id, _ := strconv.Atoi(r.PathValue("id"))
		db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
		w.WriteHeader(200)
	}
}

func handleTaskOrder(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID, _ := strconv.Atoi(r.PathValue("id"))
		newPos, _ := strconv.Atoi(r.FormValue("position"))

		// Get task's phase
		var phaseID sql.NullInt64
		var projectID int
		err := db.QueryRow(`SELECT project_id, phase_id FROM tasks WHERE id = ?`, taskID).Scan(&projectID, &phaseID)
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
