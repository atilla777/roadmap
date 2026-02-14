#!/usr/bin/env bash
set -euo pipefail

# Check Go is available
if ! command -v go &>/dev/null; then
  echo "Error: go is not installed or not in PATH" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$HOME/.claude/skills/roadmap"
BIN="$INSTALL_DIR/roadmap"

# Build
echo "Building roadmap..."
cd "$SCRIPT_DIR"
go build -o roadmap .

# Install binary
mkdir -p "$INSTALL_DIR"
cp roadmap "$BIN"
chmod +x "$BIN"

# Generate SKILL.md
cat > "$INSTALL_DIR/SKILL.md" <<EOF
---
name: roadmap
description: Project task and roadmap management. Use when the user asks about tasks, what to work on next, or project progress.
---

## Current state

\`\`\`
\`$BIN context\`
\`\`\`

## Available commands

All commands use the \`$BIN\` binary. The project is auto-detected by cwd, or use \`-p <name>\` to specify explicitly.

### Project management

| Command | Description |
|---------|-------------|
| \`roadmap project add "name" [--path /dir]\` | Create a project (path defaults to cwd) |
| \`roadmap project list\` | List all projects |
| \`roadmap project remove "name"\` | Delete project and all its tasks |

### Task management

| Command | Description |
|---------|-------------|
| \`roadmap add "title" [--phase "Phase"]\` | Add a pending task |
| \`roadmap start <id>\` | Mark task as active |
| \`roadmap done [id]\` | Mark as done (no id = single active) |
| \`roadmap current\` | Show active tasks |
| \`roadmap next\` | Next 5 pending tasks |
| \`roadmap list\` | All tasks grouped by phase |
| \`roadmap context\` | Compact LLM summary |
| \`roadmap edit <id> --title/--phase\` | Edit a task |
| \`roadmap remove <id>\` | Delete a task |

## Workflow

1. **Create project**: \`roadmap project add "myproject"\` (once per project, from project root)
2. **Pick a task**: \`roadmap next\` to see what's pending
3. **Start it**: \`roadmap start <id>\`
4. **Do the work**: implement the task
5. **Mark done**: \`roadmap done\` (auto-picks single active task)
6. **Repeat**: check \`roadmap next\` for the next one
EOF

echo "Installed successfully!"
echo "  Binary: $BIN"
echo "  Skill:  $INSTALL_DIR/SKILL.md"
