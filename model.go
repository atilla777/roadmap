package main

import "database/sql"

type Project struct {
	ID        int
	Name      string
	Path      string
	CreatedAt string
}

type Task struct {
	ID        int
	ProjectID int
	Title     string
	Status    string
	Phase     string
	CreatedAt string
	UpdatedAt string
}

func scanProject(row *sql.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Path, &p.CreatedAt)
	return p, err
}

func scanProjects(rows *sql.Rows) ([]Project, error) {
	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func scanTask(row *sql.Row) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Phase, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func scanTasks(rows *sql.Rows) ([]Task, error) {
	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Phase, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
