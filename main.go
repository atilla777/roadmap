package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func printUsage() {
	fmt.Println(`roadmap — project task tracker

global flags:
  -p <project>              use named project (default: auto-detect by cwd)

project commands:
  project add "name" [--path /dir]  create a project (path defaults to cwd)
  project list                      list all projects
  project edit "name" --name/--desc edit project
  project remove "name"             delete project and its tasks

phase commands:
  phase add "title" [--desc "..."]   create a phase
  phase list                        list phases in order
  phase edit <id> --title/--desc    edit a phase
  phase remove <id>                 delete phase (tasks move to backlog)
  phase move <id> <position>        reorder a phase

task commands:
  add "title" [--phase "P"] [--desc "..."]  add a pending task
  start <id>                  mark task as active
  done [id]                   mark task as done (default: single active)
  current                     show active tasks
  next                        show next 5 pending tasks
  list                        all tasks grouped by phase
  context                     compact LLM summary
  edit <id> --title/--phase/--desc  edit a task
  move <id> <position>        reorder a task within its phase
  remove <id>                 delete a task

web:
  serve [--port 8080]         start web interface`)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	// Parse global -p flag before command dispatch
	var projectFlag string
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "-p" {
		projectFlag = args[1]
		args = args[2:]
	}

	if len(args) < 1 {
		printUsage()
		os.Exit(0)
	}

	db := mustDB()
	defer db.Close()

	cmd := args[0]
	rest := args[1:]

	var err error

	// Project subcommands don't need project resolution
	if cmd == "project" {
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: roadmap project <add|list|remove> ...")
			os.Exit(1)
		}
		switch rest[0] {
		case "add":
			err = cmdProjectAdd(db, rest[1:])
		case "list":
			err = cmdProjectList(db)
		case "edit":
			err = cmdProjectEdit(db, rest[1:])
		case "remove":
			err = cmdProjectRemove(db, rest[1:])
		default:
			fmt.Fprintf(os.Stderr, "unknown project command: %s\n", rest[0])
			os.Exit(1)
		}
		if err != nil {
			fatal(err)
		}
		return
	}

	// Serve command
	if cmd == "serve" {
		cmdServe(db, rest)
		return
	}

	// Resolve project for task/phase commands
	proj := resolveProject(db, projectFlag)

	// Phase subcommands
	if cmd == "phase" {
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "usage: roadmap phase <add|list|remove|move> ...")
			os.Exit(1)
		}
		switch rest[0] {
		case "add":
			err = cmdPhaseAdd(db, proj.ID, rest[1:])
		case "list":
			err = cmdPhaseList(db, proj.ID)
		case "edit":
			err = cmdPhaseEdit(db, proj.ID, rest[1:])
		case "remove":
			err = cmdPhaseRemove(db, proj.ID, rest[1:])
		case "move":
			err = cmdPhaseMove(db, proj.ID, rest[1:])
		default:
			fmt.Fprintf(os.Stderr, "unknown phase command: %s\n", rest[0])
			os.Exit(1)
		}
		if err != nil {
			fatal(err)
		}
		return
	}

	switch cmd {
	case "add":
		err = cmdAdd(db, proj.ID, rest)
	case "start":
		err = cmdStart(db, proj.ID, rest)
	case "done":
		err = cmdDone(db, proj.ID, rest)
	case "current":
		err = cmdCurrent(db, proj.ID)
	case "next":
		err = cmdNext(db, proj.ID)
	case "list":
		err = cmdList(db, proj.ID)
	case "context":
		err = cmdContext(db, proj.ID, proj.Name)
	case "edit":
		err = cmdEdit(db, proj.ID, rest)
	case "move":
		err = cmdMove(db, proj.ID, rest)
	case "remove":
		err = cmdRemove(db, proj.ID, rest)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
	if err != nil {
		fatal(err)
	}
}

func resolveProject(db *sql.DB, flagName string) Project {
	if flagName != "" {
		row := db.QueryRow(`SELECT id, name, path, description, created_at FROM projects WHERE name = ?`, flagName)
		p, err := scanProject(row)
		if err != nil {
			fmt.Fprintf(os.Stderr, "project %q not found\n", flagName)
			os.Exit(1)
		}
		return p
	}

	// Auto-detect by cwd
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting cwd: %v\n", err)
		os.Exit(1)
	}

	rows, err := db.Query(`SELECT id, name, path, description, created_at FROM projects WHERE path != '' ORDER BY length(path) DESC`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	projects, err := scanProjects(rows)
	rows.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Find project whose path is the longest prefix of cwd
	cwdClean := filepath.Clean(cwd)
	for _, p := range projects {
		prefix := filepath.Clean(p.Path)
		if cwdClean == prefix || strings.HasPrefix(cwdClean, prefix+string(filepath.Separator)) {
			return p
		}
	}

	fmt.Fprintln(os.Stderr, "no project matches cwd; use -p <name> or: roadmap project add ...")
	os.Exit(1)
	return Project{} // unreachable
}
