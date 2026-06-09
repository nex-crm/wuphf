package team

// broker_agent_files_http.go owns the HTTP surface the web UI uses to VIEW and
// EDIT an agent's instruction files (SOUL / IDENTITY / OPERATIONS / TOOLS) and
// the office-wide USER.md. Two endpoints:
//
//	GET  /agent-files/read?path=agents/{slug}/SOUL.md
//	POST /agent-files/write   { path, content, commit_message, expected_sha }
//
// Design + security notes
// =======================
//
//   - These are deliberately SEPARATE from the /wiki/* endpoints. They route
//     through the agent-file storage layer (agent_files.go), which validates
//     with the strict validateAgentFilePath allowlist — agents/{slug}/{canonical
//     file}.md and office/USER.md only — and never regenerates the team/ article
//     index. Reusing /wiki/write would have widened the 20-caller
//     validateArticlePath gate AND pushed instruction files into the team
//     catalog. A dedicated, tightly-scoped surface is the smaller, safer change.
//   - The write path is HTTP-only (not exposed via any MCP tool) and is gated
//     two ways: humanRouteAllowed keeps the human/web token to this path, AND
//     handleAgentFileWrite hard-requires a human-session actor. The second check
//     matters because broker-token actors bypass humanRouteAllowed — without it
//     a compromised agent holding the broker token could rewrite its own (or
//     another agent's) SOUL/IDENTITY, i.e. prompt-inject via self-modification.
//     The committing identity is resolved server-side so attribution cannot be
//     forged.
//   - Optimistic concurrency mirrors /wiki/write-human exactly: the client sends
//     the per-file expected_sha it opened against; a 409 carries the current SHA
//     and bytes so the editor can prompt re-apply without a second round trip.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// agentFileReadResponse is the JSON shape returned by GET /agent-files/read.
type agentFileReadResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA     string `json:"sha"`
	// Exists is false when no file has been committed to disk yet — Content
	// then carries the deterministic seed so the editor opens with real text,
	// and the first save (with expected_sha == "") creates the file.
	Exists bool `json:"exists"`
}

// handleAgentFileRead returns one agent instruction file's content + SHA.
//
//	GET /agent-files/read?path=agents/{slug}/SOUL.md
//
// When the file has not been backfilled to disk yet, the handler returns the
// deterministic seed content (exists:false, sha:"") so the editor never opens
// blank. The path is validated by the strict agent-file allowlist.
func (b *Broker) handleAgentFileRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateAgentFilePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	data, err := worker.AgentFileRead(relPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(data) > 0 {
		sha, err := worker.Repo().AgentFileSHA(r.Context(), relPath)
		if err != nil {
			// A committed file with an unresolvable SHA is a real backend fault
			// (corrupt repo / git failure). Surface it rather than returning an
			// empty SHA, which would let the editor open as if the file had no
			// history and silently risk a stale overwrite.
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "resolve sha: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, agentFileReadResponse{
			Path:    relPath,
			Content: string(data),
			SHA:     strings.TrimSpace(sha),
			Exists:  true,
		})
		return
	}

	// File absent on disk — seed the editor with the deterministic content so
	// it is never blank. The first save persists it (expected_sha == "").
	writeJSON(w, http.StatusOK, agentFileReadResponse{
		Path:    relPath,
		Content: b.agentFileSeedContent(relPath),
		SHA:     "",
		Exists:  false,
	})
}

// agentFileSeedContent renders the deterministic seed for an absent instruction
// file so the read endpoint can return real text instead of a blank editor.
// Returns "" when the slug is not a current roster member (the editor then
// opens empty, which is the correct fallback for a stale path).
func (b *Broker) agentFileSeedContent(relPath string) string {
	clean := strings.TrimSpace(relPath)
	if clean == officeUserFileRel {
		return renderOfficeUserFile()
	}
	parts := strings.Split(clean, "/")
	if len(parts) != 3 || parts[0] != "agents" {
		return ""
	}
	slug := parts[1]
	name := strings.TrimSuffix(parts[2], ".md")
	members := b.OfficeMembers()
	leadSlug, _ := leadSlugAndName(members)
	for _, m := range members {
		if strings.TrimSpace(m.Slug) == slug {
			return renderAgentFileContent(m, name, slug == strings.TrimSpace(leadSlug))
		}
	}
	return ""
}

// handleAgentFileWrite saves a human edit to one agent instruction file.
//
//	POST /agent-files/write
//	{ "path": "agents/ceo/SOUL.md", "content": "...",
//	  "commit_message": "human: tighten boundaries", "expected_sha": "abc123" }
//
// Responses mirror /wiki/write-human:
//
//	200 { "path", "commit_sha", "bytes_written" }
//	400 { "error" }            malformed JSON / bad path / empty content
//	409 { "error", "current_sha", "current_content" }   concurrent write
//	503 { "error" }            wiki backend not active
//	500 { "error" }            other failure
func (b *Broker) handleAgentFileWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Hard-require a human-session actor. humanRouteAllowed already keeps the
	// human/web token to this path, but broker-token actors bypass that check —
	// an agent must never rewrite an instruction file (its own or another's),
	// since those files are loaded into the system prompt. Writes are human-only.
	actor, ok := requestActorFromContext(r.Context())
	if !ok || actor.Kind != requestActorKindHuman {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "human session required"})
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	// Cap the body: instruction files are small, and an unbounded decode on a
	// fast loopback connection could exhaust memory before the read timeout.
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	var body struct {
		Path          string `json:"path"`
		Content       string `json:"content"`
		CommitMessage string `json:"commit_message"`
		ExpectedSHA   string `json:"expected_sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	// Pre-validate BEFORE the commit so a rejection never touches the tree.
	if err := validateAgentFilePath(body.Path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	// actor is a verified human (checked above); resolve its git identity so the
	// commit is attributed server-side and cannot be forged by the client.
	identity := humanIdentityFromActor(actor)

	sha, n, err := worker.Repo().CommitAgentFileHuman(
		r.Context(), body.Path, body.Content, body.ExpectedSHA, body.CommitMessage, identity,
	)
	if err != nil {
		if errors.Is(err, ErrWikiSHAMismatch) {
			// Return the current bytes so the editor can show the reload prompt
			// without a second round trip. sha carries the current SHA here.
			current, _ := worker.AgentFileRead(body.Path)
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":           err.Error(),
				"current_sha":     sha,
				"current_content": string(current),
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":          body.Path,
		"commit_sha":    sha,
		"bytes_written": n,
	})
}
