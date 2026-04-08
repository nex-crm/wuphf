# PR Blueprint 03: Worktree Isolation & Parallel Swarms

## 1. Objective
Enable specialists to work in parallel "labs" (isolated directories) without corrupting the main workspace or conflicting with each other.

## 2. Key Features
- **Git Worktree Support:** When a task is assigned, the agent should have the option to spin up a temporary worktree.
- **Direct Agent-to-Agent Messaging (`SendMessage`):** A tool that allows one agent to signal another (e.g., "BE Engineer tells FE: The user-auth endpoint is ready").
- **Task Rigor:** Tasks should have IDs and be trackable in the status bar (e.g., "2 Tasks Running").

## 3. Targeted Files
- `internal/agent/delegator.go`: Handle the logic for creating/tearing down worktrees.
- `internal/agent/tools.go`: Add `send_message` (inter-agent).
- `internal/teammcp/server.go`: Expose task counts and isolation status to the agents.

## 4. Implementation Details
- **Worktree Management:** Use `git worktree add /tmp/wuphf-task-<id> <branch>`. Ensure the directory is deleted after the task is "Committed" or "Canceled."
- **Isolation Scope:** Tools like `Read` and `Write` must respect the `CWD` (Current Working Directory) of the agent's worktree.
- **Swarm ID:** Each isolated task should have a unique ID that correlates to the tmux pane and the disk logs.

## 5. Validation
- Trigger a "Large Feature" that requires the FE and BE engineers to work simultaneously.
- Verify they are working in separate `/tmp` directories.
- Verify they can use `send_message` to coordinate their progress.
