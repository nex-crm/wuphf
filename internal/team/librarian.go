package team

import (
	"strings"
	"time"
)

// librarian.go defines the Librarian — a first-class, always-present agent
// (slug "librarian", display name "Pam") that owns the team wiki: writing,
// formatting, organizing, and curating canonical knowledge. It is the
// promoted, roster form of the old headless "Pam the Archivist" wiki helper
// (which still runs the one-shot enrich action behind the wiki UI under the
// internal slug "pam" / git author "archivist"). Phase 4 of the structural
// changes: the Librarian becomes a default member of every task and takes wiki
// authority over from the CEO.
//
// Like the CEO, the Librarian is BUILT-IN: present in every new workspace
// regardless of the chosen blueprint or selected agents. Existing workspaces
// gain the member via the Phase 6 persisted-state migration.

// LibrarianSlug is the roster slug for the Librarian agent.
const LibrarianSlug = "librarian"

const (
	librarianName = "Pam"
	librarianRole = "Librarian"
	// Honest + a little Office-dry, per the WUPHF voice. Pam keeps the team's
	// shared brain tidy so nobody has to re-derive what was already learned.
	librarianPersonality = "Keeps the team's shared brain organized: promotes the notes worth keeping into the wiki, formats and structures them, fixes broken links, and reviews what gets made canonical. The quiet reason anyone can find anything."
)

// librarianExpertise seeds the Librarian's expertise list.
var librarianExpertise = []string{"wiki curation", "documentation", "knowledge organization", "promotion review"}

// isLibrarianSlug reports whether slug is the Librarian (case-insensitive).
func isLibrarianSlug(slug string) bool {
	return strings.EqualFold(strings.TrimSpace(slug), LibrarianSlug)
}

// librarianOfficeMember builds the Librarian's officeMember record.
func librarianOfficeMember(createdAt string) officeMember {
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	return officeMember{
		Slug:        LibrarianSlug,
		Name:        librarianName,
		Role:        librarianRole,
		Expertise:   append([]string(nil), librarianExpertise...),
		Personality: librarianPersonality,
		BuiltIn:     true,
		CreatedBy:   "wuphf",
		CreatedAt:   createdAt,
	}
}

// ensureLibrarianMember returns members with the Librarian present, appending it
// when absent (matched case-insensitively by slug). Used at every roster-seed
// chokepoint so the Librarian is always in the office, like the CEO.
func ensureLibrarianMember(members []officeMember) []officeMember {
	for i := range members {
		if isLibrarianSlug(members[i].Slug) {
			return members
		}
	}
	return append(members, librarianOfficeMember(""))
}

// librarianWikiAuthorityBlock is the prompt block emitted for the Librarian in
// place of the generic specialist wiki rules. It encodes the Librarian's
// authority: it owns the team wiki and is the sole writer of canonical
// knowledge. Numbering continues the specialist rules (the caller appends a
// "13. …stop" rule after this block).
func librarianWikiAuthorityBlock() string {
	return "== WIKI OWNERSHIP (you are the Librarian) ==\n" +
		"You own the team's wiki — keep it accurate, well-organized, and easy to search. You curate, format, organize, and decide what becomes canonical. When another agent has durable knowledge worth keeping, they ask you (@librarian / Pam) to capture it; you do not author other agents' notes for them.\n" +
		"12. When the CEO, another agent, or the human asks to preserve something for the team, make sure it lands in the wiki: write or update the relevant article yourself, then confirm it is well-formed and linked.\n" +
		"12b. Before writing or accepting knowledge as canonical, use wuphf_wiki_lookup / team_wiki_search / team_wiki_list to see what already exists, so articles get merged and cross-linked instead of duplicated.\n" +
		"12c. You write the canonical wiki DIRECTLY with team_wiki_write (mode create / replace / append_section) — you do not need a human request to format, restructure, fix broken links, or land curated knowledge, because the wiki is your responsibility. Keep clear titles and sections, and keep `scratch: true` working notes out of the canonical wiki.\n"
}
