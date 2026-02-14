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
	mustExec(db, `DELETE FROM projects WHERE id = ?`, p.ID)
	fmt.Printf("project %q removed\n", name)
}

// --- Task commands ---

func cmdAdd(db *sql.DB, projectID int, args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	phase := fs.String("phase", "", "task phase")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap add "title" [--phase "Phase"]`)
		os.Exit(1)
	}
	title := fs.Arg(0)

	res, err := db.Exec(
		`INSERT INTO tasks (project_id, title, phase) VALUES (?, ?, ?)`,
		projectID, title, *phase,
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
			`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? AND status = 'active'`,
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
		`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? AND status = 'active' ORDER BY id`,
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
		if t.Phase != "" {
			phase = fmt.Sprintf(" [%s]", t.Phase)
		}
		fmt.Printf("#%d %s%s\n", t.ID, t.Title, phase)
	}
}

func cmdNext(db *sql.DB, projectID int) {
	rows, err := db.Query(
		`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? AND status = 'pending' ORDER BY id LIMIT 5`,
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
		if t.Phase != "" {
			phase = fmt.Sprintf(" [%s]", t.Phase)
		}
		fmt.Printf("#%d %s%s\n", t.ID, t.Title, phase)
	}
}

func cmdList(db *sql.DB, projectID int) {
	rows, err := db.Query(
		`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? ORDER BY phase, id`,
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
	fmt.Print(FormatList(tasks))
}

func cmdContext(db *sql.DB, projectID int, projectName string) {
	doneRows, err := db.Query(
		`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? AND status = 'done' ORDER BY updated_at DESC LIMIT 3`,
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
		`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? AND status = 'active' ORDER BY id`,
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
		`SELECT id, project_id, title, status, phase, created_at, updated_at FROM tasks WHERE project_id = ? AND status = 'pending' ORDER BY id LIMIT 5`,
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
	phase := fs.String("phase", "", "new phase")
	fs.Parse(reorderArgs(args))

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, `usage: roadmap edit <id> [--title "..."] [--phase "..."]`)
		os.Exit(1)
	}
	id := mustParseID(fs.Arg(0))

	if *title == "" && *phase == "" {
		fmt.Fprintln(os.Stderr, "specify --title or --phase")
		os.Exit(1)
	}

	if *title != "" {
		mustExec(db, `UPDATE tasks SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, *title, id, projectID)
	}
	if *phase != "" {
		mustExec(db, `UPDATE tasks SET phase = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, *phase, id, projectID)
	}
	fmt.Printf("#%d updated\n", id)
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

// helpers

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
