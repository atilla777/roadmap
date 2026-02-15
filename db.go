package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func dbPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	dir := filepath.Join(home, ".claude", "skills", "roadmap")
	os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, ".roadmap.db")
}

func OpenDB() (*sql.DB, error) {
	path := dbPath()
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

const currentSchemaVersion = 3

func Migrate(db *sql.DB) error {
	// Ensure schema_version table exists
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	if err != nil {
		return fmt.Errorf("migrate: create schema_version: %w", err)
	}

	var version int
	err = db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if err == sql.ErrNoRows {
		version = 0
	} else if err != nil {
		return fmt.Errorf("migrate: read version: %w", err)
	}

	if version < 1 {
		if err := migrateV1(db); err != nil {
			return err
		}
	}
	if version < 2 {
		if err := migrateV2(db); err != nil {
			return err
		}
	}
	if version < 3 {
		if err := migrateV3(db); err != nil {
			return err
		}
	}

	// Upsert schema version
	if version == 0 {
		_, err = db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, currentSchemaVersion)
	} else {
		_, err = db.Exec(`UPDATE schema_version SET version = ?`, currentSchemaVersion)
	}
	if err != nil {
		return fmt.Errorf("migrate: update version: %w", err)
	}
	return nil
}

// migrateV1 creates the original schema (projects + tasks with text phase).
func migrateV1(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    path       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now'))
);

CREATE TABLE IF NOT EXISTS tasks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    title      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','done')),
    phase      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now'))
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_phase ON tasks(phase);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate v1: %w", err)
	}
	return nil
}

// migrateV2 introduces phases as a first-class entity and adds sort_order to tasks.
func migrateV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migrate v2: begin: %w", err)
	}
	defer tx.Rollback()

	// Create phases table
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS phases (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    title      TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now')),
    UNIQUE(project_id, title)
)`)
	if err != nil {
		return fmt.Errorf("migrate v2: create phases: %w", err)
	}

	// Check if tasks table has the old 'phase' TEXT column
	hasOldPhase := false
	rows, err := tx.Query(`PRAGMA table_info(tasks)`)
	if err != nil {
		return fmt.Errorf("migrate v2: pragma: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("migrate v2: scan pragma: %w", err)
		}
		if name == "phase" && typ == "TEXT" {
			hasOldPhase = true
		}
	}
	rows.Close()

	if hasOldPhase {
		// Migrate existing phase text values into the phases table
		_, err = tx.Exec(`
INSERT OR IGNORE INTO phases (project_id, title, sort_order)
SELECT DISTINCT project_id, phase,
    ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY MIN(id)) - 1
FROM tasks
WHERE phase != ''
GROUP BY project_id, phase`)
		if err != nil {
			return fmt.Errorf("migrate v2: migrate phase data: %w", err)
		}

		// Recreate tasks table with new schema
		_, err = tx.Exec(`
CREATE TABLE tasks_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    title      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','done')),
    phase_id   INTEGER REFERENCES phases(id),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now'))
)`)
		if err != nil {
			return fmt.Errorf("migrate v2: create tasks_new: %w", err)
		}

		// Copy data, resolving phase text to phase_id
		_, err = tx.Exec(`
INSERT INTO tasks_new (id, project_id, title, status, phase_id, sort_order, created_at, updated_at)
SELECT t.id, t.project_id, t.title, t.status,
    p.id,
    ROW_NUMBER() OVER (PARTITION BY t.project_id, COALESCE(p.id, 0) ORDER BY t.id) - 1,
    t.created_at, t.updated_at
FROM tasks t
LEFT JOIN phases p ON p.project_id = t.project_id AND p.title = t.phase`)
		if err != nil {
			return fmt.Errorf("migrate v2: copy tasks: %w", err)
		}

		_, err = tx.Exec(`DROP TABLE tasks`)
		if err != nil {
			return fmt.Errorf("migrate v2: drop old tasks: %w", err)
		}

		_, err = tx.Exec(`ALTER TABLE tasks_new RENAME TO tasks`)
		if err != nil {
			return fmt.Errorf("migrate v2: rename tasks: %w", err)
		}
	} else {
		// Fresh install or already migrated — just ensure columns exist
		_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    title      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','done')),
    phase_id   INTEGER REFERENCES phases(id),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%S','now'))
)`)
		if err != nil {
			return fmt.Errorf("migrate v2: create tasks: %w", err)
		}
	}

	// Recreate indexes for new schema
	_, err = tx.Exec(`
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_phase_id ON tasks(phase_id);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_phases_project ON phases(project_id);
`)
	if err != nil {
		return fmt.Errorf("migrate v2: create indexes: %w", err)
	}

	return tx.Commit()
}

// migrateV3 adds description (markdown) to tasks, projects, and phases.
func migrateV3(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE tasks ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE phases ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("migrate v3: %w", err)
		}
	}
	return nil
}

func mustDB() *sql.DB {
	db, err := OpenDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := Migrate(db); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return db
}
