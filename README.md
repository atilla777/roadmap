# roadmap

Claude Code skill for project task management. Tracks tasks, phases, and progress across multiple projects using a local SQLite database.

## Installation

```bash
git clone https://github.com/atilla777/roadmap.git
cd roadmap
./install.sh
```

This builds the binary and installs it along with `SKILL.md` to `~/.claude/skills/roadmap/`.

## Requirements

- Go 1.25+
- Claude Code

## Commands

### Project management

```bash
roadmap project add "myproject"        # create project (uses cwd as path)
roadmap project add "other" --path /x  # create with explicit path
roadmap project list                   # list all projects
roadmap project remove "myproject"     # delete project and its tasks
```

### Task management

```bash
roadmap add "Implement auth"               # add a pending task
roadmap add "Write tests" --phase "v2"     # add task to a phase
roadmap start 1                            # mark task as active
roadmap done                               # mark single active task as done
roadmap done 1                             # mark specific task as done
roadmap current                            # show active tasks
roadmap next                               # next 5 pending tasks
roadmap list                               # all tasks grouped by phase
roadmap context                            # compact summary for LLM
roadmap edit 1 --title "New title"         # edit task title
roadmap edit 1 --phase "v2"               # move task to phase
roadmap remove 1                           # delete task
```

## Usage in Claude Code

Once installed, use `/roadmap` in Claude Code. The skill auto-detects the current project by working directory.
