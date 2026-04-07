# WUPHF vs CC-agent: Full System Analysis & Strategy

## 1. Product Philosophy: The "Office" vs The "Tool"
WUPHF's strength is **Coordination**. CC-agent's strength is **Iteration**.
- **WUPHF Identity:** A shared TUI where agents talk in channels. It feels "alive."
- **CC-agent Identity:** A surgical instrument for code. It feels "precise."
- **The Synthesis:** WUPHF should remain an Office, but the "Teammates" should use CC-agent's professional tools.

## 2. Key Gaps in WUPHF
- **Execution:** WUPHF agents can "search Nex" but they can't effectively `grep` code or `edit` files locally.
- **Rendering:** Long tool outputs (build logs, large file reads) flood the Bubble Tea stream, making it unreadable.
- **Isolation:** If two agents work on the same repo, they overwrite each other's files.
- **Context:** Long office threads eventually result in "context window exceeded" errors with no recovery path.

## 3. High-Value "Borrow" List from CC-agent
- **Summary Rendering:** Show tool results as 1-line "folded" summaries in the TUI.
- **Fuzzy Pickers:** Use searchable lists for switching channels or mentioning agents.
- **Git Worktrees:** Create a temporary `/tmp/wuphf-worktree-...` for an agent to work in isolation.
- **Reactive Compaction:** Automatically summarize old messages when the window is 80% full.

## 4. "Do Not Copy" List
- **Ink/React TUI:** WUPHF stays in Go/Bubble Tea for performance and binary simplicity.
- **Solo-First UX:** WUPHF must prioritize "Public" work in channels over private agent-only side-talk.
- **Hidden Tasks:** Keep work in visible TMUX panes, but summarize the *results* in the TUI.
