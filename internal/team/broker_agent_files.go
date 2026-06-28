package team

import (
	"context"
	"log"
	"strings"
	"time"
)

// broker_agent_files.go wires the per-agent instruction file set (SOUL /
// IDENTITY / OPERATIONS / TOOLS + office USER) into the broker: reads for the
// prompt builder, and a deterministic backfill that seeds the files for every
// agent in the roster. See agent_files.go for the storage + generation layer.

// ReadAgentInstruction returns the content of one of an agent's instruction
// files (name = SOUL|IDENTITY|OPERATIONS|TOOLS), or "" when the wiki backend is
// off or the file is absent. Safe for the prompt hot path (single disk read
// under the repo lock).
func (b *Broker) ReadAgentInstruction(slug, name string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || !isAgentInstructionFileName(strings.TrimSpace(name)) {
		return ""
	}
	worker := b.WikiWorker()
	if worker == nil {
		return ""
	}
	data, err := worker.AgentFileRead(agentFileRel(slug, name))
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
}

// ReadOfficeUserFile returns the office-wide USER.md content, or "" when absent.
func (b *Broker) ReadOfficeUserFile() string {
	worker := b.WikiWorker()
	if worker == nil {
		return ""
	}
	data, err := worker.AgentFileRead(officeUserFileRel)
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
}

// backfillAgentFilesForRoster seeds any MISSING instruction file for every agent
// in the roster (plus the office USER.md) with deterministic content derived
// from the agent's current persona/role/expertise/tools. Idempotent: existing
// files are never overwritten, so a human's edits survive. Runs on every
// roster-ensure hook so new agents and fresh offices get their files
// without an LLM call (which could half-initialize an agent on failure).
func (b *Broker) backfillAgentFilesForRoster() {
	worker := b.WikiWorker()
	if worker == nil {
		return
	}
	members := b.OfficeMembers()
	if len(members) == 0 {
		return
	}
	leadSlug, _ := leadSlugAndName(members)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writeIfAbsent := func(author, relPath, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		if existing, err := worker.AgentFileRead(relPath); err == nil && len(existing) > 0 {
			return // never clobber existing content (human edits or prior seed)
		}
		if _, _, err := worker.AgentFileWrite(ctx, author, relPath, content, "create", "agent: seed "+relPath); err != nil {
			// A create race ("already exists") or a transient git error is
			// non-fatal: the next roster-ensure retries, and a present file is
			// the desired end state anyway.
			if !strings.Contains(err.Error(), "already exists") {
				log.Printf("agent file: seed %s failed: %v", relPath, err)
			}
		}
	}

	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		slug := strings.TrimSpace(member.Slug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		isLead := slug == strings.TrimSpace(leadSlug)
		writeIfAbsent(slug, agentFileRel(slug, "SOUL"), renderAgentSoul(member, isLead))
		writeIfAbsent(slug, agentFileRel(slug, "IDENTITY"), renderAgentIdentity(member))
		writeIfAbsent(slug, agentFileRel(slug, "OPERATIONS"), renderAgentOperations(member, isLead))
		writeIfAbsent(slug, agentFileRel(slug, "TOOLS"), renderAgentTools(member))
	}

	// Office-wide human-context file (one per office), authored by the lead or
	// a bootstrap identity.
	author := strings.TrimSpace(leadSlug)
	if author == "" {
		author = "wuphf-bootstrap"
	}
	writeIfAbsent(author, officeUserFileRel, renderOfficeUserFile())
}
