package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
)

// --- Project commands ---

func cmdProjectAdd(db *sql.DB, args []string) {
	fs := flag.NewFlagSet("project add", flag.ExitOnError)
	pathFlag := fs.String("path", "", "project directory (default: cwd)")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap project add "name" [--path /dir]`)
		os.Exit(1)
	}
	name := fs.Arg(0)

	p := *pathFlag
	if p == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting cwd: %v\n", err)
			os.Exit(1)
		}
		p = cwd
	}

	_, err := db.Exec(`INSERT INTO projects (name, path) VALUES (?, ?)`, name, p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("project %q created (path: %s)\n", name, p)
}

func cmdProjectList(db *sql.DB) {
	rows, err := db.Query(`SELECT id, name, path, created_at FROM projects ORDER BY name`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	projects, err := scanProjects(rows)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(projects) == 0 {
		fmt.Println("no projects")
		return
	}
	for _, p := range projects {
		path := p.Path
		if path == "" {
			path = "(no path)"
		}
		fmt.Printf("%-20s %s\n", p.Name, path)
	}
}

func cmdProjectRemove(db *sql.DB, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap project remove "name"`)
		os.Exit(1)
	}
	name := args[0]

	row := db.QueryRow(`SELECT id, name, path, created_at FROM projects WHERE name = ?`, name)
	p, err := scanProject(row)
	if err != nil {
		fmt.Fprintf(os.Stderr, "project %q not found\n", name)
		os.Exit(1)
	}

	mustExec(db, `DELETE FROM tasks WHERE project_id = ?`, p.ID)
	mustExec(db, `DELETE FROM phases WHERE project_id = ?`, p.ID)
	mustExec(db, `DELETE FROM projects WHERE id = ?`, p.ID)
	fmt.Printf("project %q removed\n", name)
}

// --- Phase commands ---

func cmdPhaseAdd(db *sql.DB, projectID int, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap phase add "title"`)
		os.Exit(1)
	}
	title := args[0]

	// Get max sort_order for this project
	var maxOrder int
	err := db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM phases WHERE project_id = ?`, projectID).Scan(&maxOrder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	res, err := db.Exec(
		`INSERT INTO phases (project_id, title, sort_order) VALUES (?, ?, ?)`,
		projectID, title, maxOrder+1,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	id, _ := res.LastInsertId()
	fmt.Printf("phase #%d %q created\n", id, title)
}

func cmdPhaseList(db *sql.DB, projectID int) {
	rows, err := db.Query(
		`SELECT id, project_id, title, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	phases, err := scanPhases(rows)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(phases) == 0 {
		fmt.Println("no phases")
		return
	}
	for _, p := range phases {
		// Count tasks in this phase
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE phase_id = ?`, p.ID).Scan(&count)
		fmt.Printf("#%-3d %s  (%d tasks)\n", p.ID, p.Title, count)
	}
}

func cmdPhaseRemove(db *sql.DB, projectID int, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: roadmap phase remove <id>")
		os.Exit(1)
	}
	id := mustParseID(args[0])

	// Verify phase belongs to project
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ? AND project_id = ?`, id, projectID).Scan(&exists)
	if err != nil || exists == 0 {
		fmt.Fprintf(os.Stderr, "phase #%d not found\n", id)
		os.Exit(1)
	}

	// Move tasks to backlog (phase_id = NULL)
	mustExec(db, `UPDATE tasks SET phase_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE phase_id = ?`, id)
	mustExec(db, `DELETE FROM phases WHERE id = ?`, id)
	fmt.Printf("phase #%d removed (tasks moved to backlog)\n", id)
}

func cmdPhaseMove(db *sql.DB, projectID int, args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: roadmap phase move <id> <position>")
		os.Exit(1)
	}
	id := mustParseID(args[0])
	newPos := mustParseID(args[1])

	// Get all phases for the project ordered by sort_order
	rows, err := db.Query(
		`SELECT id, project_id, title, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	phases, err := scanPhases(rows)
	rows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Find current index
	currentIdx := -1
	for i, p := range phases {
		if p.ID == id {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		fmt.Fprintf(os.Stderr, "phase #%d not found\n", id)
		os.Exit(1)
	}

	// Clamp position
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= len(phases) {
		newPos = len(phases) - 1
	}

	// Reorder: remove from current position, insert at new position
	phase := phases[currentIdx]
	phases = append(phases[:currentIdx], phases[currentIdx+1:]...)
	rear := make([]Phase, len(phases[newPos:]))
	copy(rear, phases[newPos:])
	phases = append(phases[:newPos], phase)
	phases = append(phases, rear...)

	// Update sort_order for all phases
	for i, p := range phases {
		mustExec(db, `UPDATE phases SET sort_order = ? WHERE id = ?`, i, p.ID)
	}
	fmt.Printf("phase #%d moved to position %d\n", id, newPos)
}

// --- Task commands ---

func cmdAdd(db *sql.DB, projectID int, args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	phase := fs.String("phase", "", "phase (ID or title)")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap add "title" [--phase "Phase"]`)
		os.Exit(1)
	}
	title := fs.Arg(0)

	var phaseID sql.NullInt64
	if *phase != "" {
		phaseID = resolvePhase(db, projectID, *phase)
	}

	// Get max sort_order for this phase group
	var maxOrder int
	if phaseID.Valid {
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE project_id = ? AND phase_id = ?`, projectID, phaseID.Int64).Scan(&maxOrder)
	} else {
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE project_id = ? AND phase_id IS NULL`, projectID).Scan(&maxOrder)
	}

	res, err := db.Exec(
		`INSERT INTO tasks (project_id, title, phase_id, sort_order) VALUES (?, ?, ?, ?)`,
		projectID, title, phaseID, maxOrder+1,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	id, _ := res.LastInsertId()
	fmt.Printf("added #%d: %s\n", id, title)
}

func cmdStart(db *sql.DB, projectID int, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: roadmap start <id>")
		os.Exit(1)
	}
	id := mustParseID(args[0])
	mustUpdateStatus(db, projectID, id, "active")
	fmt.Printf("#%d → active\n", id)
}

func cmdDone(db *sql.DB, projectID int, args []string) {
	var id int
	if len(args) >= 1 {
		id = mustParseID(args[0])
	} else {
		rows, err := db.Query(
			`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'active'`,
			projectID,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		tasks, err := scanTasks(rows)
		rows.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		switch len(tasks) {
		case 0:
			fmt.Fprintln(os.Stderr, "no active tasks")
			os.Exit(1)
		case 1:
			id = tasks[0].ID
		default:
			fmt.Fprintln(os.Stderr, "multiple active tasks, specify id:")
			for _, t := range tasks {
				fmt.Fprintf(os.Stderr, "  #%d %s\n", t.ID, t.Title)
			}
			os.Exit(1)
		}
	}
	mustUpdateStatus(db, projectID, id, "done")
	fmt.Printf("#%d → done\n", id)
}

func cmdCurrent(db *sql.DB, projectID int) {
	rows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'active' ORDER BY t.id`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Println("no active tasks")
		return
	}
	for _, t := range tasks {
		phase := ""
		if t.PhaseTitle != "" {
			phase = fmt.Sprintf(" [%s]", t.PhaseTitle)
		}
		fmt.Printf("#%d %s%s\n", t.ID, t.Title, phase)
	}
}

func cmdNext(db *sql.DB, projectID int) {
	rows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'pending' ORDER BY t.sort_order, t.id LIMIT 5`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Println("no pending tasks")
		return
	}
	for _, t := range tasks {
		phase := ""
		if t.PhaseTitle != "" {
			phase = fmt.Sprintf(" [%s]", t.PhaseTitle)
		}
		fmt.Printf("#%d %s%s\n", t.ID, t.Title, phase)
	}
}

func cmdList(db *sql.DB, projectID int) {
	// Get phases in order
	phaseRows, err := db.Query(
		`SELECT id, project_id, title, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	phases, err := scanPhases(phaseRows)
	phaseRows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Get tasks ordered by phase sort_order, then task sort_order
	rows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+`
		WHERE t.project_id = ?
		ORDER BY COALESCE(p.sort_order, 999999), t.sort_order, t.id`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return
	}
	fmt.Print(FormatList(phases, tasks))
}

func cmdContext(db *sql.DB, projectID int, projectName string) {
	doneRows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'done' ORDER BY t.updated_at DESC LIMIT 3`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	done, err := scanTasks(doneRows)
	doneRows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	activeRows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'active' ORDER BY t.id`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	active, err := scanTasks(activeRows)
	activeRows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	nextRows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'pending' ORDER BY t.sort_order, t.id LIMIT 5`,
		projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	next, err := scanTasks(nextRows)
	nextRows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(FormatContext(projectName, done, active, next))
}

func cmdEdit(db *sql.DB, projectID int, args []string) {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	title := fs.String("title", "", "new title")
	phase := fs.String("phase", "", "phase (ID, title, or empty to clear)")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap edit <id> [--title "..."] [--phase "..."]`)
		os.Exit(1)
	}
	id := mustParseID(fs.Arg(0))

	phaseSet := false
	for _, a := range args {
		if a == "--phase" {
			phaseSet = true
			break
		}
	}

	if *title == "" && !phaseSet {
		fmt.Fprintln(os.Stderr, "specify --title or --phase")
		os.Exit(1)
	}

	if *title != "" {
		mustExec(db, `UPDATE tasks SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, *title, id, projectID)
	}
	if phaseSet {
		if *phase == "" {
			// Clear phase (move to backlog)
			mustExec(db, `UPDATE tasks SET phase_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, id, projectID)
		} else {
			phaseID := resolvePhase(db, projectID, *phase)
			mustExec(db, `UPDATE tasks SET phase_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, phaseID, id, projectID)
		}
	}
	fmt.Printf("#%d updated\n", id)
}

func cmdMove(db *sql.DB, projectID int, args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: roadmap move <task_id> <position>")
		os.Exit(1)
	}
	taskID := mustParseID(args[0])
	newPos := mustParseID(args[1])

	// Get the task to find its phase
	row := db.QueryRow(`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.id = ? AND t.project_id = ?`, taskID, projectID)
	task, err := scanTask(row)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task #%d not found\n", taskID)
		os.Exit(1)
	}

	// Get all tasks in same phase, ordered by sort_order
	var rows *sql.Rows
	if task.PhaseID.Valid {
		rows, err = db.Query(
			`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.phase_id = ? ORDER BY t.sort_order, t.id`,
			projectID, task.PhaseID.Int64,
		)
	} else {
		rows, err = db.Query(
			`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.phase_id IS NULL ORDER BY t.sort_order, t.id`,
			projectID,
		)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	tasks, err := scanTasks(rows)
	rows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Find current index
	currentIdx := -1
	for i, t := range tasks {
		if t.ID == taskID {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		fmt.Fprintf(os.Stderr, "task #%d not found in phase\n", taskID)
		os.Exit(1)
	}

	if newPos < 0 {
		newPos = 0
	}
	if newPos >= len(tasks) {
		newPos = len(tasks) - 1
	}

	// Reorder
	t := tasks[currentIdx]
	tasks = append(tasks[:currentIdx], tasks[currentIdx+1:]...)
	rear := make([]Task, len(tasks[newPos:]))
	copy(rear, tasks[newPos:])
	tasks = append(tasks[:newPos], t)
	tasks = append(tasks, rear...)

	for i, t := range tasks {
		mustExec(db, `UPDATE tasks SET sort_order = ? WHERE id = ?`, i, t.ID)
	}
	fmt.Printf("#%d moved to position %d\n", taskID, newPos)
}

func cmdRemove(db *sql.DB, projectID int, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: roadmap remove <id>")
		os.Exit(1)
	}
	id := mustParseID(args[0])
	res, err := db.Exec(`DELETE FROM tasks WHERE id = ? AND project_id = ?`, id, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fmt.Fprintf(os.Stderr, "task #%d not found\n", id)
		os.Exit(1)
	}
	fmt.Printf("#%d removed\n", id)
}

// --- helpers ---

// resolvePhase resolves a phase argument (ID or title) to a phase_id.
func resolvePhase(db *sql.DB, projectID int, arg string) sql.NullInt64 {
	// Try as numeric ID first
	if id, err := strconv.Atoi(arg); err == nil {
		var exists int
		err := db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ? AND project_id = ?`, id, projectID).Scan(&exists)
		if err == nil && exists > 0 {
			return sql.NullInt64{Int64: int64(id), Valid: true}
		}
	}
	// Try as title
	var id int
	err := db.QueryRow(`SELECT id FROM phases WHERE project_id = ? AND title = ?`, projectID, arg).Scan(&id)
	if err == nil {
		return sql.NullInt64{Int64: int64(id), Valid: true}
	}
	fmt.Fprintf(os.Stderr, "phase %q not found\n", arg)
	os.Exit(1)
	return sql.NullInt64{}
}

func reorderArgs(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if len(args[i]) > 1 && args[i][0] == '-' {
			flags = append(flags, args[i])
			if i+1 < len(args) && (len(args[i+1]) == 0 || args[i+1][0] != '-') {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}

func mustParseID(s string) int {
	id, err := strconv.Atoi(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid id: %s\n", s)
		os.Exit(1)
	}
	return id
}

func mustUpdateStatus(db *sql.DB, projectID, id int, status string) {
	res, err := db.Exec(
		`UPDATE tasks SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`,
		status, id, projectID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fmt.Fprintf(os.Stderr, "task #%d not found\n", id)
		os.Exit(1)
	}
}

func mustExec(db *sql.DB, query string, args ...any) {
	_, err := db.Exec(query, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
