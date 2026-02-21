package main

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createProject(t *testing.T, db *sql.DB, name, path string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO projects (name, path) VALUES (?, ?)`, name, path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func createPhase(t *testing.T, db *sql.DB, projectID int, title string, sortOrder int) int {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO phases (project_id, title, sort_order) VALUES (?, ?, ?)`,
		projectID, title, sortOrder,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func createTask(t *testing.T, db *sql.DB, projectID int, title, status string, phaseID *int) int {
	t.Helper()
	var pid sql.NullInt64
	if phaseID != nil {
		pid = sql.NullInt64{Int64: int64(*phaseID), Valid: true}
	}
	res, err := db.Exec(
		`INSERT INTO tasks (project_id, title, status, phase_id) VALUES (?, ?, ?, ?)`,
		projectID, title, status, pid,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func intPtr(i int) *int { return &i }
