# roadmap

[Claude Code](https://docs.anthropic.com/en/docs/claude-code) skill for project task management.

Tracks tasks, phases, and progress across multiple projects. Data stored in a local SQLite database (`~/.roadmap.db`). The skill auto-detects the current project by working directory.

Once installed, type `/roadmap` in Claude Code and the agent will see your current tasks, suggest what to work on next, and update task status as you go.

## Requirements

- Go 1.25+
- Claude Code

## Installation

### By human

```bash
git clone https://github.com/atilla777/roadmap.git
cd roadmap
./install.sh
```

`install.sh` builds the binary and copies it along with `SKILL.md` to `~/.claude/skills/roadmap/`. After that `/roadmap` is available in Claude Code.

### By AI agent

Ask Claude Code:

> Install the roadmap skill from https://github.com/atilla777/roadmap.git — clone the repo to /tmp, run ./install.sh, then remove the clone.

Or provide step-by-step instructions:

```
1. git clone https://github.com/atilla777/roadmap.git /tmp/roadmap
2. cd /tmp/roadmap && ./install.sh
3. rm -rf /tmp/roadmap
```

After installation the skill is available via `/roadmap` command.

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

## Typical workflow

```bash
roadmap project add "myproject"   # once, from the project root
roadmap add "Set up CI"           # plan tasks
roadmap add "Write API" --phase "v1"
roadmap start 1                   # begin working
roadmap done                      # finish
roadmap next                      # what's next?
```
