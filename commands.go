package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// --- validation ---

const (
	maxTitleLen = 255
	maxNameLen  = 255
	maxDescLen  = 10000
)

func validateTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title must not be empty")
	}
	if len(title) > maxTitleLen {
		return fmt.Errorf("title too long (max %d characters)", maxTitleLen)
	}
	return nil
}

func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("name too long (max %d characters)", maxNameLen)
	}
	return nil
}

func validateDesc(desc string) error {
	if len(desc) > maxDescLen {
		return fmt.Errorf("description too long (max %d characters)", maxDescLen)
	}
	return nil
}

func validatePriority(s string) (int, error) {
	switch strings.ToLower(s) {
	case "none", "0", "":
		return 0, nil
	case "low", "1":
		return 1, nil
	case "medium", "med", "2":
		return 2, nil
	case "high", "3":
		return 3, nil
	default:
		return 0, fmt.Errorf("invalid priority %q (use none/low/medium/high or 0-3)", s)
	}
}

func validateDueDate(s string) error {
	if s == "" {
		return nil
	}
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return fmt.Errorf("invalid due date %q (use YYYY-MM-DD)", s)
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return fmt.Errorf("invalid due date %q (use YYYY-MM-DD)", s)
		}
	}
	return nil
}

// --- helpers ---

func parseID(s string) (int, error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %s", s)
	}
	return id, nil
}

func execDB(db *sql.DB, query string, args ...any) error {
	_, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	return nil
}

func updateStatus(db *sql.DB, projectID, id int, status string) error {
	res, err := db.Exec(
		`UPDATE tasks SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`,
		status, id, projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task #%d not found", id)
	}
	return nil
}

// resolvePhase resolves a phase argument (ID or title) to a phase_id.
func resolvePhase(db *sql.DB, projectID int, arg string) (sql.NullInt64, error) {
	// Try as numeric ID first
	if id, err := strconv.Atoi(arg); err == nil {
		var exists int
		err := db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ? AND project_id = ?`, id, projectID).Scan(&exists)
		if err == nil && exists > 0 {
			return sql.NullInt64{Int64: int64(id), Valid: true}, nil
		}
	}
	// Try as title
	var id int
	err := db.QueryRow(`SELECT id FROM phases WHERE project_id = ? AND title = ?`, projectID, arg).Scan(&id)
	if err == nil {
		return sql.NullInt64{Int64: int64(id), Valid: true}, nil
	}
	return sql.NullInt64{}, fmt.Errorf("phase %q not found", arg)
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

// --- Project commands ---

func cmdProjectAdd(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("project add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pathFlag := fs.String("path", "", "project directory (default: cwd)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf(`usage: roadmap project add "name" [--path /dir]`)
	}
	name := fs.Arg(0)
	if err := validateName(name); err != nil {
		return err
	}

	p := *pathFlag
	if p == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("error getting cwd: %w", err)
		}
		p = cwd
	}

	_, err := db.Exec(`INSERT INTO projects (name, path) VALUES (?, ?)`, name, p)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	fmt.Printf("project %q created (path: %s)\n", name, p)
	return nil
}

func cmdProjectList(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, name, path, description, created_at FROM projects ORDER BY name`)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	defer rows.Close()
	projects, err := scanProjects(rows)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	if len(projects) == 0 {
		fmt.Println("no projects")
		return nil
	}
	for _, p := range projects {
		path := p.Path
		if path == "" {
			path = "(no path)"
		}
		fmt.Printf("%-20s %s\n", p.Name, path)
	}
	return nil
}

func cmdProjectEdit(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("project edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "new project name")
	desc := fs.String("desc", "", "project description (markdown)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf(`usage: roadmap project edit "name" [--name "..."] [--desc "..."]`)
	}
	current := fs.Arg(0)

	row := db.QueryRow(`SELECT id, name, path, description, created_at FROM projects WHERE name = ?`, current)
	p, err := scanProject(row)
	if err != nil {
		return fmt.Errorf("project %q not found", current)
	}

	nameSet := false
	descSet := false
	for _, a := range args {
		if a == "--name" {
			nameSet = true
		}
		if a == "--desc" {
			descSet = true
		}
	}

	if !nameSet && !descSet {
		return fmt.Errorf("specify --name or --desc")
	}

	if nameSet {
		if err := validateName(*name); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE projects SET name = ? WHERE id = ?`, *name, p.ID); err != nil {
			return err
		}
	}
	if descSet {
		if err := validateDesc(*desc); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE projects SET description = ? WHERE id = ?`, *desc, p.ID); err != nil {
			return err
		}
	}
	fmt.Printf("project %q updated\n", current)
	return nil
}

func cmdProjectRemove(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf(`usage: roadmap project remove "name"`)
	}
	name := args[0]

	row := db.QueryRow(`SELECT id, name, path, description, created_at FROM projects WHERE name = ?`, name)
	p, err := scanProject(row)
	if err != nil {
		return fmt.Errorf("project %q not found", name)
	}

	if err := execDB(db, `DELETE FROM tasks WHERE project_id = ?`, p.ID); err != nil {
		return err
	}
	if err := execDB(db, `DELETE FROM phases WHERE project_id = ?`, p.ID); err != nil {
		return err
	}
	if err := execDB(db, `DELETE FROM projects WHERE id = ?`, p.ID); err != nil {
		return err
	}
	fmt.Printf("project %q removed\n", name)
	return nil
}

// --- Phase commands ---

func cmdPhaseAdd(db *sql.DB, projectID int, args []string) error {
	fs := flag.NewFlagSet("phase add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	desc := fs.String("desc", "", "description (markdown)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf(`usage: roadmap phase add "title" [--desc "..."]`)
	}
	title := fs.Arg(0)
	if err := validateTitle(title); err != nil {
		return err
	}
	if err := validateDesc(*desc); err != nil {
		return err
	}

	var maxOrder int
	err := db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM phases WHERE project_id = ?`, projectID).Scan(&maxOrder)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	res, err := db.Exec(
		`INSERT INTO phases (project_id, title, description, sort_order) VALUES (?, ?, ?, ?)`,
		projectID, title, *desc, maxOrder+1,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	id, _ := res.LastInsertId()
	fmt.Printf("phase #%d %q created\n", id, title)
	return nil
}

func cmdPhaseList(db *sql.DB, projectID int) error {
	rows, err := db.Query(
		`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	defer rows.Close()
	phases, err := scanPhases(rows)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	if len(phases) == 0 {
		fmt.Println("no phases")
		return nil
	}
	for _, p := range phases {
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE phase_id = ?`, p.ID).Scan(&count)
		fmt.Printf("#%-3d %s  (%d tasks)\n", p.ID, p.Title, count)
	}
	return nil
}

func cmdPhaseEdit(db *sql.DB, projectID int, args []string) error {
	fs := flag.NewFlagSet("phase edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	title := fs.String("title", "", "new title")
	desc := fs.String("desc", "", "description (markdown)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf(`usage: roadmap phase edit <id> [--title "..."] [--desc "..."]`)
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}

	var exists int
	err = db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ? AND project_id = ?`, id, projectID).Scan(&exists)
	if err != nil || exists == 0 {
		return fmt.Errorf("phase #%d not found", id)
	}

	titleSet := false
	descSet := false
	for _, a := range args {
		if a == "--title" {
			titleSet = true
		}
		if a == "--desc" {
			descSet = true
		}
	}

	if !titleSet && !descSet {
		return fmt.Errorf("specify --title or --desc")
	}

	if titleSet {
		if err := validateTitle(*title); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE phases SET title = ? WHERE id = ?`, *title, id); err != nil {
			return err
		}
	}
	if descSet {
		if err := validateDesc(*desc); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE phases SET description = ? WHERE id = ?`, *desc, id); err != nil {
			return err
		}
	}
	fmt.Printf("phase #%d updated\n", id)
	return nil
}

func cmdPhaseRemove(db *sql.DB, projectID int, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: roadmap phase remove <id>")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}

	var exists int
	err = db.QueryRow(`SELECT COUNT(*) FROM phases WHERE id = ? AND project_id = ?`, id, projectID).Scan(&exists)
	if err != nil || exists == 0 {
		return fmt.Errorf("phase #%d not found", id)
	}

	if err := execDB(db, `UPDATE tasks SET phase_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE phase_id = ?`, id); err != nil {
		return err
	}
	if err := execDB(db, `DELETE FROM phases WHERE id = ?`, id); err != nil {
		return err
	}
	fmt.Printf("phase #%d removed (tasks moved to backlog)\n", id)
	return nil
}

func cmdPhaseMove(db *sql.DB, projectID int, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: roadmap phase move <id> <position>")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	newPos, err := parseID(args[1])
	if err != nil {
		return err
	}

	rows, err := db.Query(
		`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	phases, err := scanPhases(rows)
	rows.Close()
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	currentIdx := -1
	for i, p := range phases {
		if p.ID == id {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		return fmt.Errorf("phase #%d not found", id)
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
		if err := execDB(db, `UPDATE phases SET sort_order = ? WHERE id = ?`, i, p.ID); err != nil {
			return err
		}
	}
	fmt.Printf("phase #%d moved to position %d\n", id, newPos)
	return nil
}

// --- Task commands ---

func cmdAdd(db *sql.DB, projectID int, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	phase := fs.String("phase", "", "phase (ID or title)")
	desc := fs.String("desc", "", "description (markdown)")
	priorityFlag := fs.String("priority", "", "priority (none/low/medium/high)")
	dueFlag := fs.String("due", "", "due date (YYYY-MM-DD)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf(`usage: roadmap add "title" [--phase "Phase"] [--desc "..."] [--priority low/medium/high] [--due YYYY-MM-DD]`)
	}
	title := fs.Arg(0)
	if err := validateTitle(title); err != nil {
		return err
	}
	if err := validateDesc(*desc); err != nil {
		return err
	}

	priority := 0
	if *priorityFlag != "" {
		var err error
		priority, err = validatePriority(*priorityFlag)
		if err != nil {
			return err
		}
	}
	if err := validateDueDate(*dueFlag); err != nil {
		return err
	}

	var phaseID sql.NullInt64
	if *phase != "" {
		var err error
		phaseID, err = resolvePhase(db, projectID, *phase)
		if err != nil {
			return err
		}
	}

	var maxOrder int
	if phaseID.Valid {
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE project_id = ? AND phase_id = ?`, projectID, phaseID.Int64).Scan(&maxOrder)
	} else {
		db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM tasks WHERE project_id = ? AND phase_id IS NULL`, projectID).Scan(&maxOrder)
	}

	res, err := db.Exec(
		`INSERT INTO tasks (project_id, title, description, phase_id, sort_order, priority, due_date) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		projectID, title, *desc, phaseID, maxOrder+1, priority, *dueFlag,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	id, _ := res.LastInsertId()
	fmt.Printf("added #%d: %s\n", id, title)
	return nil
}

func cmdStart(db *sql.DB, projectID int, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: roadmap start <id>")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	if err := updateStatus(db, projectID, id, "active"); err != nil {
		return err
	}
	fmt.Printf("#%d → active\n", id)
	return nil
}

func cmdDone(db *sql.DB, projectID int, args []string) error {
	var id int
	if len(args) >= 1 {
		var err error
		id, err = parseID(args[0])
		if err != nil {
			return err
		}
	} else {
		rows, err := db.Query(
			`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'active'`,
			projectID,
		)
		if err != nil {
			return fmt.Errorf("error: %w", err)
		}
		tasks, err := scanTasks(rows)
		rows.Close()
		if err != nil {
			return fmt.Errorf("error: %w", err)
		}
		switch len(tasks) {
		case 0:
			return fmt.Errorf("no active tasks")
		case 1:
			id = tasks[0].ID
		default:
			msg := "multiple active tasks, specify id:"
			for _, t := range tasks {
				msg += fmt.Sprintf("\n  #%d %s", t.ID, t.Title)
			}
			return fmt.Errorf("%s", msg)
		}
	}
	if err := updateStatus(db, projectID, id, "done"); err != nil {
		return err
	}
	fmt.Printf("#%d → done\n", id)
	return nil
}

func cmdCurrent(db *sql.DB, projectID int) error {
	rows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'active' ORDER BY t.id`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	if len(tasks) == 0 {
		fmt.Println("no active tasks")
		return nil
	}
	for _, t := range tasks {
		phase := ""
		if t.PhaseTitle != "" {
			phase = fmt.Sprintf(" [%s]", t.PhaseTitle)
		}
		extra := taskExtra(t)
		fmt.Printf("#%d %s%s%s\n", t.ID, t.Title, phase, extra)
		if t.Description != "" {
			fmt.Print(indentDesc(t.Description, "   "))
		}
	}
	return nil
}

func cmdNext(db *sql.DB, projectID int) error {
	rows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'pending' ORDER BY t.sort_order, t.id LIMIT 5`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	if len(tasks) == 0 {
		fmt.Println("no pending tasks")
		return nil
	}
	for _, t := range tasks {
		phase := ""
		if t.PhaseTitle != "" {
			phase = fmt.Sprintf(" [%s]", t.PhaseTitle)
		}
		extra := taskExtra(t)
		fmt.Printf("#%d %s%s%s\n", t.ID, t.Title, phase, extra)
		if t.Description != "" {
			fmt.Print(indentDesc(t.Description, "   "))
		}
	}
	return nil
}

func cmdList(db *sql.DB, projectID int, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statusFilter := fs.String("status", "", "filter by status (pending/active/done)")
	phaseFilter := fs.String("phase", "", "filter by phase (ID or title)")
	searchFilter := fs.String("search", "", "search title and description")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	phaseRows, err := db.Query(
		`SELECT id, project_id, title, description, sort_order, created_at FROM phases WHERE project_id = ? ORDER BY sort_order`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	phases, err := scanPhases(phaseRows)
	phaseRows.Close()
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	where := `WHERE t.project_id = ?`
	params := []any{projectID}

	if *statusFilter != "" {
		where += ` AND t.status = ?`
		params = append(params, *statusFilter)
	}
	if *phaseFilter != "" {
		phaseID, err := resolvePhase(db, projectID, *phaseFilter)
		if err != nil {
			return err
		}
		where += ` AND t.phase_id = ?`
		params = append(params, phaseID.Int64)
	}
	if *searchFilter != "" {
		where += ` AND (t.title LIKE ? OR t.description LIKE ?)`
		q := "%" + *searchFilter + "%"
		params = append(params, q, q)
	}

	rows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` `+where+`
		ORDER BY COALESCE(p.sort_order, 999999), t.sort_order, t.id`,
		params...,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return nil
	}
	fmt.Print(FormatList(phases, tasks))
	return nil
}

func cmdContext(db *sql.DB, projectID int, projectName string) error {
	doneRows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'done' ORDER BY t.updated_at DESC LIMIT 3`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	done, err := scanTasks(doneRows)
	doneRows.Close()
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	activeRows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'active' ORDER BY t.id`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	active, err := scanTasks(activeRows)
	activeRows.Close()
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	nextRows, err := db.Query(
		`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.project_id = ? AND t.status = 'pending' ORDER BY t.sort_order, t.id LIMIT 5`,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	next, err := scanTasks(nextRows)
	nextRows.Close()
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	fmt.Print(FormatContext(projectName, done, active, next))
	return nil
}

func cmdEdit(db *sql.DB, projectID int, args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	title := fs.String("title", "", "new title")
	phase := fs.String("phase", "", "phase (ID, title, or empty to clear)")
	desc := fs.String("desc", "", "description (markdown)")
	priorityFlag := fs.String("priority", "", "priority (none/low/medium/high)")
	dueFlag := fs.String("due", "", "due date (YYYY-MM-DD or empty to clear)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf(`usage: roadmap edit <id> [--title "..."] [--phase "..."] [--desc "..."] [--priority ...] [--due YYYY-MM-DD]`)
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}

	phaseSet := false
	descSet := false
	prioritySet := false
	dueSet := false
	for _, a := range args {
		switch a {
		case "--phase":
			phaseSet = true
		case "--desc":
			descSet = true
		case "--priority":
			prioritySet = true
		case "--due":
			dueSet = true
		}
	}

	if *title == "" && !phaseSet && !descSet && !prioritySet && !dueSet {
		return fmt.Errorf("specify --title, --phase, --desc, --priority, or --due")
	}

	if *title != "" {
		if err := validateTitle(*title); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE tasks SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, *title, id, projectID); err != nil {
			return err
		}
	}
	if descSet {
		if err := validateDesc(*desc); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE tasks SET description = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, *desc, id, projectID); err != nil {
			return err
		}
	}
	if prioritySet {
		p, err := validatePriority(*priorityFlag)
		if err != nil {
			return err
		}
		if err := execDB(db, `UPDATE tasks SET priority = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, p, id, projectID); err != nil {
			return err
		}
	}
	if dueSet {
		if err := validateDueDate(*dueFlag); err != nil {
			return err
		}
		if err := execDB(db, `UPDATE tasks SET due_date = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, *dueFlag, id, projectID); err != nil {
			return err
		}
	}
	if phaseSet {
		if *phase == "" {
			if err := execDB(db, `UPDATE tasks SET phase_id = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, id, projectID); err != nil {
				return err
			}
		} else {
			phaseID, err := resolvePhase(db, projectID, *phase)
			if err != nil {
				return err
			}
			if err := execDB(db, `UPDATE tasks SET phase_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%S','now') WHERE id = ? AND project_id = ?`, phaseID, id, projectID); err != nil {
				return err
			}
		}
	}
	fmt.Printf("#%d updated\n", id)
	return nil
}

func cmdMove(db *sql.DB, projectID int, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: roadmap move <task_id> <position>")
	}
	taskID, err := parseID(args[0])
	if err != nil {
		return err
	}
	newPos, err := parseID(args[1])
	if err != nil {
		return err
	}

	row := db.QueryRow(`SELECT `+taskSelectCols+` `+taskFromJoin+` WHERE t.id = ? AND t.project_id = ?`, taskID, projectID)
	task, err := scanTask(row)
	if err != nil {
		return fmt.Errorf("task #%d not found", taskID)
	}

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
		return fmt.Errorf("error: %w", err)
	}
	tasks, err := scanTasks(rows)
	rows.Close()
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}

	currentIdx := -1
	for i, t := range tasks {
		if t.ID == taskID {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		return fmt.Errorf("task #%d not found in phase", taskID)
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
		if err := execDB(db, `UPDATE tasks SET sort_order = ? WHERE id = ?`, i, t.ID); err != nil {
			return err
		}
	}
	fmt.Printf("#%d moved to position %d\n", taskID, newPos)
	return nil
}

func cmdRemove(db *sql.DB, projectID int, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: roadmap remove <id>")
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM tasks WHERE id = ? AND project_id = ?`, id, projectID)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task #%d not found", id)
	}
	fmt.Printf("#%d removed\n", id)
	return nil
}
