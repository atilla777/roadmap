package main

import "database/sql"

type Project struct {
	ID          int
	Name        string
	Path        string
	Description string
	CreatedAt   string
}

type Phase struct {
	ID          int
	ProjectID   int
	Title       string
	Description string
	SortOrder   int
	CreatedAt   string
}

type Task struct {
	ID          int
	ProjectID   int
	Title       string
	Description string
	Status      string
	PhaseID     sql.NullInt64
	SortOrder   int
	CreatedAt   string
	UpdatedAt   string
	// Joined field, not stored directly
	PhaseTitle string
}

func scanProject(row *sql.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt)
	return p, err
}

func scanProjects(rows *sql.Rows) ([]Project, error) {
	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func scanPhase(row *sql.Row) (Phase, error) {
	var p Phase
	err := row.Scan(&p.ID, &p.ProjectID, &p.Title, &p.Description, &p.SortOrder, &p.CreatedAt)
	return p, err
}

func scanPhases(rows *sql.Rows) ([]Phase, error) {
	var phases []Phase
	for rows.Next() {
		var p Phase
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Title, &p.Description, &p.SortOrder, &p.CreatedAt); err != nil {
			return nil, err
		}
		phases = append(phases, p)
	}
	return phases, rows.Err()
}

func scanTask(row *sql.Row) (Task, error) {
	var t Task
	var phaseTitle sql.NullString
	err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.PhaseID, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt, &phaseTitle)
	if phaseTitle.Valid {
		t.PhaseTitle = phaseTitle.String
	}
	return t, err
}

func scanTasks(rows *sql.Rows) ([]Task, error) {
	var tasks []Task
	for rows.Next() {
		var t Task
		var phaseTitle sql.NullString
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.PhaseID, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt, &phaseTitle); err != nil {
			return nil, err
		}
		if phaseTitle.Valid {
			t.PhaseTitle = phaseTitle.String
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// taskSelectCols is the standard SELECT for tasks with a LEFT JOIN on phases.
const taskSelectCols = `t.id, t.project_id, t.title, t.description, t.status, t.phase_id, t.sort_order, t.created_at, t.updated_at, p.title`
const taskFromJoin = `FROM tasks t LEFT JOIN phases p ON p.id = t.phase_id`
