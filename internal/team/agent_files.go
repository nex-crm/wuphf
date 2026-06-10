package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// agent_files.go owns the per-agent instruction file set that makes an agent's
// "soul" real, inspectable, and editable. Each agent has, under the wiki git
// repo at agents/{slug}/:
//
//   - SOUL.md       — persona, values, voice, boundaries
//   - IDENTITY.md   — name, slug, role, expertise, runtime
//   - OPERATIONS.md — how the agent works + escalation (the AGENTS.md role)
//   - TOOLS.md      — tool inventory + usage notes
//
// plus one office-wide office/USER.md describing the single human the office
// serves. Memory is the Notebook subsystem (agents/{slug}/notebook/), so there
// is no MEMORY.md; cadence is the scheduler, so there is no HEARTBEAT.md.
//
// These files live in the same git repo as notebooks and team articles, so they
// are versioned, diffable, and Librarian-visible. They are loaded into the
// agent's system prompt at build time (see prompt_builder.go). This file mirrors
// the notebook write/read path (CommitNotebook / NotebookRead) so agent files
// get the same OS-lock + git-commit guarantees without polluting the team/
// article index.

// agentInstructionFiles is the canonical per-agent file set, in prompt order.
var agentInstructionFiles = []string{"SOUL", "IDENTITY", "OPERATIONS", "TOOLS"}

// officeUserFileRel is the office-wide human-context file (one per office).
const officeUserFileRel = "office/USER.md"

// agentFileRel builds agents/{slug}/{NAME}.md for a per-agent instruction file.
func agentFileRel(slug, name string) string {
	return "agents/" + strings.TrimSpace(slug) + "/" + strings.TrimSpace(name) + ".md"
}

// isAgentInstructionFileName reports whether name is one of the canonical
// per-agent file names (SOUL/IDENTITY/OPERATIONS/TOOLS).
func isAgentInstructionFileName(name string) bool {
	for _, f := range agentInstructionFiles {
		if f == name {
			return true
		}
	}
	return false
}

// aiGeneratableFile reports whether an instruction file is prose worth handing
// to an LLM to author. IDENTITY and TOOLS are factual (name/role/runtime,
// tool inventory) and are derived deterministically from member data, so AI
// generation is deliberately NOT offered for them — only SOUL and OPERATIONS
// (and, separately, the office USER file) carry prose the model improves.
func aiGeneratableFile(name string) bool {
	return name == "SOUL" || name == "OPERATIONS"
}

// agentFilePurpose returns a one-line description of what a file governs, used
// to brief the LLM when it authors a richer version.
func agentFilePurpose(name string) string {
	switch name {
	case "SOUL":
		return "the agent's persona, values, voice, and hard boundaries"
	case "OPERATIONS":
		return "how the agent works day to day and when it escalates"
	case "USER":
		return "the single human this office serves and how to optimize for their time"
	default:
		return "the agent's instructions"
	}
}

// stripMarkdownFences removes a single wrapping ```...``` code fence if the
// model returned one despite being told not to. Idempotent on unfenced input.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		s = strings.TrimPrefix(s, "```")
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// validateAgentFilePath allows ONLY the canonical agent-instruction files:
// agents/{slug}/{SOUL|IDENTITY|OPERATIONS|TOOLS}.md and office/USER.md. Anything
// else (arbitrary agents/ paths, traversal, the notebook subtree) is rejected,
// so this path can never be used to clobber a notebook entry or escape the repo.
func validateAgentFilePath(relPath string) error {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return fmt.Errorf("agent file: path is required")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("agent file: path must be relative; got %q", relPath)
	}
	// Reject any "." path segment outright (raw, before Clean collapses it).
	// The file names are fixed and slugs never contain dots, so a legitimate
	// path has no "." or ".." segment — refusing them keeps one agent from
	// addressing another's file via a normalizing path.
	if strings.Contains(relPath, "..") {
		return fmt.Errorf("agent file: path must not contain ..; got %q", relPath)
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	// Require the input to already be canonical. Cleaning collapses inputs like
	// "agents/ceo/SOUL.md/." or a trailing slash to a valid in-tree path, but the
	// write/read callers use the RAW relPath for the filesystem op — so a
	// non-canonical input that only passes after Clean could create unintended
	// directories or fail mid-write. Reject the divergence outright.
	if clean != filepath.ToSlash(relPath) {
		return fmt.Errorf("agent file: path must be canonical; got %q", relPath)
	}
	if clean == officeUserFileRel {
		return nil
	}
	parts := strings.Split(clean, "/")
	if len(parts) != 3 || parts[0] != "agents" {
		return fmt.Errorf("agent file: path must be agents/{slug}/{FILE}.md or %s; got %q", officeUserFileRel, relPath)
	}
	if err := validateNotebookSlug(parts[1]); err != nil {
		return err
	}
	name := strings.TrimSuffix(parts[2], ".md")
	if name == parts[2] || !isAgentInstructionFileName(name) {
		return fmt.Errorf("agent file: name must be one of %v (.md); got %q", agentInstructionFiles, parts[2])
	}
	return nil
}

// commitAgentFileLocked writes + commits one agent instruction file. Mirrors
// commitNotebookLocked: it does NOT regenerate the team/ article index (agent
// files are per-agent and do not feed the team catalog). Caller holds r.mu.
func (r *Repo) commitAgentFileLocked(ctx context.Context, slug, relPath, content, mode, message string) (string, int, error) {
	if err := validateAgentFilePath(relPath); err != nil {
		return "", 0, err
	}
	author := strings.TrimSpace(slug)
	if author == "" {
		author = "wuphf-bootstrap"
	}
	fullPath := filepath.Join(r.root, relPath)

	switch mode {
	case "create":
		if _, err := os.Stat(fullPath); err == nil {
			return "", 0, fmt.Errorf("agent file: already exists at %q; use replace", relPath)
		}
	case "replace":
		// overwrite fine
	default:
		return "", 0, fmt.Errorf("agent file: unknown write mode %q; expected create|replace", mode)
	}
	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("agent file: content is required")
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("agent file: mkdir %s: %w", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("agent file: write %s: %w", relPath, err)
	}
	bytesWritten := len(content)

	relForGit := filepath.ToSlash(relPath)
	if out, err := r.runGitLocked(ctx, author, "add", "--", relForGit); err != nil {
		return "", 0, fmt.Errorf("agent file: git add %s: %w: %s", relPath, err, out)
	}
	cachedDiff, err := r.runGitLocked(ctx, "system", "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("agent file: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if err != nil {
			return "", 0, fmt.Errorf("agent file: resolve HEAD sha: %w", err)
		}
		return strings.TrimSpace(headSha), bytesWritten, nil
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("agent: update %s", relPath)
	}
	if out, err := r.runGitLocked(ctx, author, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("agent file: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, author, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("agent file: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), bytesWritten, nil
}

// CommitAgentFile writes + commits one agent instruction file under r.mu.
func (r *Repo) CommitAgentFile(ctx context.Context, slug, relPath, content, mode, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.commitAgentFileLocked(ctx, slug, relPath, content, mode, message)
}

// CommitAgentFileHuman writes one agent instruction file as a HUMAN edit with
// optimistic-concurrency: the caller passes the per-file short SHA it last saw
// and the write is rejected with ErrWikiSHAMismatch when HEAD has moved past it.
// This is the editor's save path (mirrors CommitHuman) — but, like the agent
// commit path, it does NOT regenerate the team/ article index: instruction
// files are per-agent and never feed the team catalog. Returns the new short
// SHA, bytes written, and an error.
func (r *Repo) CommitAgentFileHuman(ctx context.Context, relPath, content, expectedSHA, message string, identity HumanIdentity) (string, int, error) {
	// Resolve the effective identity before touching the filesystem so every
	// downstream git sub-call stamps the same author. Mirrors CommitHuman.
	name := strings.TrimSpace(identity.Name)
	email := strings.TrimSpace(identity.Email)
	slug := strings.TrimSpace(identity.Slug)
	if name == "" || email == "" || slug == "" {
		name = FallbackHumanIdentity.Name
		email = FallbackHumanIdentity.Email
		slug = FallbackHumanIdentity.Slug
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := validateAgentFilePath(relPath); err != nil {
		return "", 0, err
	}
	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("agent file: content is required")
	}

	fullPath := filepath.Join(r.root, relPath)
	exists := false
	if _, err := os.Stat(fullPath); err == nil {
		exists = true
	}

	// Optimistic concurrency pre-check BEFORE any filesystem mutation, so a
	// rejection leaves the working tree clean (matches CommitHuman).
	if exists {
		if expectedSHA == "" {
			curSHA, serr := r.currentArticleSHALocked(ctx, relPath)
			if serr != nil {
				return "", 0, fmt.Errorf("agent file: resolve current sha: %w", serr)
			}
			return curSHA, 0, fmt.Errorf("%w: file exists but no expected_sha supplied", ErrWikiSHAMismatch)
		}
		curSHA, serr := r.currentArticleSHALocked(ctx, relPath)
		if serr != nil {
			return "", 0, fmt.Errorf("agent file: resolve current sha: %w", serr)
		}
		if !shaEquivalent(curSHA, expectedSHA) {
			return curSHA, 0, fmt.Errorf("%w: current %s, expected %s", ErrWikiSHAMismatch, curSHA, expectedSHA)
		}
	} else if expectedSHA != "" {
		return "", 0, fmt.Errorf("%w: file not found but expected_sha supplied", ErrWikiSHAMismatch)
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("agent file: mkdir %s: %w", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("agent file: write %s: %w", relPath, err)
	}
	bytesWritten := len(content)

	relForGit := filepath.ToSlash(relPath)
	if out, err := r.runGitLockedAs(ctx, name, email, "add", "--", relForGit); err != nil {
		return "", 0, fmt.Errorf("agent file: git add %s: %w: %s", relPath, err, out)
	}
	// Byte-identical re-write short-circuits to current HEAD; mirrors CommitHuman.
	cachedDiff, err := r.runGitLockedAs(ctx, name, email, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("agent file: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("agent file: resolve HEAD sha: %w", herr)
		}
		return strings.TrimSpace(headSha), bytesWritten, nil
	}
	// Build the bare message, then stamp the "human:" provenance prefix below
	// (the agent commit path uses "agent:" instead) so git log --oneline makes
	// the author class obvious at a glance.
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("update %s", relPath)
	}
	if !strings.HasPrefix(commitMsg, "human:") {
		commitMsg = "human: " + commitMsg
	}
	if out, err := r.runGitLockedAs(ctx, name, email, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("agent file: git commit: %w: %s", err, out)
	}
	_ = slug // retained for future author_slug plumbing, mirrors CommitHuman.
	sha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("agent file: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), bytesWritten, nil
}

// AgentFileSHA returns the short SHA of the most recent commit touching the
// agent instruction file at relPath, or "" (no error) when the file has no
// commit history yet. Used to seed the editor's expected_sha on open.
func (r *Repo) AgentFileSHA(ctx context.Context, relPath string) (string, error) {
	if err := validateAgentFilePath(relPath); err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentArticleSHALocked(ctx, relPath)
}

// AgentFileRead reads one agent instruction file by repo-relative path. Returns
// (nil, nil) when the file does not exist so callers can treat "absent" as
// "empty" without special-casing os.IsNotExist.
func (w *WikiWorker) AgentFileRead(relPath string) ([]byte, error) {
	if err := validateAgentFilePath(relPath); err != nil {
		return nil, err
	}
	if w == nil || w.repo == nil {
		return nil, nil
	}
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(w.repo.Root(), relPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// AgentFileWrite validates + commits one agent instruction file via the repo.
func (w *WikiWorker) AgentFileWrite(ctx context.Context, slug, relPath, content, mode, commitMsg string) (string, int, error) {
	if w == nil || w.repo == nil {
		return "", 0, ErrWorkerStopped
	}
	if err := validateAgentFilePath(relPath); err != nil {
		return "", 0, err
	}
	return w.repo.CommitAgentFile(ctx, slug, relPath, content, mode, commitMsg)
}

// ---- Deterministic content generation -------------------------------------
//
// v1 seeds the file set from data the office already has (persona, role,
// expertise, tools, runtime) with no LLM call — so every existing agent gets a
// real, editable instruction set immediately and a generation failure can never
// half-initialize an agent. Richer LLM-authored content is a later slice.

func renderAgentSoul(member officeMember, isLead bool) string {
	persona := strings.TrimSpace(member.Personality)
	if persona == "" {
		persona = inferOfficePersonality(member.Slug, member.Role)
	}
	role := strings.TrimSpace(member.Role)
	if role == "" {
		role = member.Slug
	}
	lane := "Stay in your lane (" + role + ") on execution; route cross-cutting scope calls through the lead."
	if isLead {
		lane = "You are the lead: you coordinate, decompose, and make the final call. Delegate execution to specialists rather than doing it all yourself."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# SOUL — @%s\n\n", member.Slug)
	b.WriteString("## Who you are\n")
	b.WriteString(persona + "\n\n")
	b.WriteString("## Values\n")
	b.WriteString("- Bias to action: leave durable state (tasks, records, deliverables), not just narration.\n")
	b.WriteString("- Reuse first: an existing teammate, task, or wiki article beats creating a new one.\n")
	b.WriteString("- Tell the human the truth, including failures. Never fabricate outcomes or proof artifacts.\n\n")
	b.WriteString("## Voice\n")
	b.WriteString(teamVoiceForSlug(member.Slug) + "\n\n")
	b.WriteString("## Boundaries\n")
	b.WriteString("- " + lane + "\n")
	b.WriteString("- Do the smallest real step that moves the work; avoid substitute or busywork artifacts.\n")
	return b.String()
}

func renderAgentIdentity(member officeMember) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# IDENTITY — @%s\n\n", member.Slug)
	fmt.Fprintf(&b, "- Name: %s\n", firstNonEmpty(member.Name, member.Slug))
	fmt.Fprintf(&b, "- Slug: %s\n", member.Slug)
	fmt.Fprintf(&b, "- Role: %s\n", firstNonEmpty(member.Role, member.Slug))
	expertise := member.Expertise
	if len(expertise) == 0 {
		expertise = inferOfficeExpertise(member.Slug, member.Role)
	}
	if len(expertise) > 0 {
		fmt.Fprintf(&b, "- Expertise: %s\n", strings.Join(expertise, ", "))
	}
	runtime := strings.TrimSpace(member.Provider.Kind)
	if m := strings.TrimSpace(member.Provider.Model); m != "" {
		runtime = strings.TrimSpace(runtime + " / " + m)
	}
	if runtime != "" {
		fmt.Fprintf(&b, "- Runtime: %s\n", runtime)
	}
	if member.BuiltIn {
		b.WriteString("- Built-in: yes\n")
	}
	return b.String()
}

func renderAgentOperations(member officeMember, isLead bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# OPERATIONS — @%s\n\n", member.Slug)
	b.WriteString("## How you work\n")
	if isLead {
		b.WriteString("- Take a goal, decompose it into owned sub-tasks (team_task, parent_issue_id), assign existing specialists, and let them run in parallel. Aggregate at the parent.\n")
		b.WriteString("- Create durable task state before you broadcast a kickoff. Proposing a new agent requires explicit human approval.\n")
	} else {
		b.WriteString("- Take work from durable tasks: claim with team_task, post status as you go, then submit for review or complete. A channel reply alone does not move the work.\n")
		b.WriteString("- Work in your task's channel and worktree. Escalate scope changes to the lead.\n")
	}
	b.WriteString("- Write durable decisions to your notebook; @librarian curates notebooks into the team wiki.\n\n")
	b.WriteString("## Escalation\n")
	b.WriteString("- If blocked on something you cannot resolve, post the blocker and notify the owner/lead instead of producing a substitute artifact.\n")
	return b.String()
}

func renderAgentTools(member officeMember) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# TOOLS — @%s\n\n", member.Slug)
	b.WriteString("## Available tools\n")
	// Track whether any non-blank tool was written: an AllowedTools slice of
	// only whitespace entries has len>0 but yields no lines, which would leave
	// an empty section — fall back to the default toolset in that case too.
	wroteTool := false
	for _, t := range member.AllowedTools {
		if t = strings.TrimSpace(t); t != "" {
			fmt.Fprintf(&b, "- %s\n", t)
			wroteTool = true
		}
	}
	if !wroteTool {
		b.WriteString("- The default office toolset (team_task, team_status, team_broadcast, notebook, wiki, and any skills enabled for you).\n")
	}
	b.WriteString("\n## Notes\n")
	b.WriteString("- Prefer the smallest real action over a proof or preview artifact unless the task explicitly asks for one.\n")
	b.WriteString("- Request a skill or capability when one is missing rather than faking the result.\n")
	return b.String()
}

// renderAgentFileContent dispatches to the right deterministic generator for a
// canonical instruction file name. Returns "" for an unknown name. Used by the
// HTTP read handler to seed the editor with real content when a file has not
// been backfilled to disk yet (the first save then persists it).
func renderAgentFileContent(member officeMember, name string, isLead bool) string {
	switch name {
	case "SOUL":
		return renderAgentSoul(member, isLead)
	case "IDENTITY":
		return renderAgentIdentity(member)
	case "OPERATIONS":
		return renderAgentOperations(member, isLead)
	case "TOOLS":
		return renderAgentTools(member)
	default:
		return ""
	}
}

func renderOfficeUserFile() string {
	var b strings.Builder
	b.WriteString("# USER — the human this office serves\n\n")
	if ctx := strings.TrimSpace(config.CompanyContextBlock()); ctx != "" {
		b.WriteString(ctx)
		if !strings.HasSuffix(ctx, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("This WUPHF office serves a single human operator. Optimize for their time:\n")
	b.WriteString("- Handle routine work autonomously; surface only the decisions that genuinely need them.\n")
	b.WriteString("- Be candid. Report outcomes, tradeoffs, and failures plainly.\n")
	b.WriteString("- One human owns approvals (plan gates, new-agent creation, external actions). Wait for them on those.\n")
	return b.String()
}
