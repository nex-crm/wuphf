package team

import (
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/operations"
)

// librarian.go defines the Librarian — a first-class, always-present agent
// (slug "librarian", display name "Pam") that owns the team wiki: writing,
// formatting, organizing, and reviewing notebook→wiki promotions. It is the
// promoted, roster form of the old headless "Pam the Archivist" wiki helper
// (which still runs the one-shot enrich action behind the wiki UI under the
// internal slug "pam" / git author "archivist"). Phase 4 of the structural
// changes: the Librarian becomes a default member of every task and takes wiki
// promotion/review authority over from the CEO.
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

// librarianAwareReviewer wraps a base wiki-promotion reviewer resolver so the
// Librarian becomes the DEFAULT reviewer (Phase 4 authority move). When the base
// resolver returns nothing or its historical fallback ("ceo") — i.e. the
// blueprint did not pin a specific reviewer for the path — and the workspace has
// a Librarian, route the review to the Librarian. A blueprint-configured
// reviewer is respected. Existing workspaces without a Librarian member
// (pre-Phase-6) keep their current reviewer, so this is safe to roll out.
//
// (Blueprints use "ceo" only as the implicit fallback, never as an explicit
// DefaultReviewer, so treating base=="ceo" as "unset" does not steal a
// deliberately-chosen CEO reviewer.)
func librarianAwareReviewer(b *Broker, base ReviewerResolver) ReviewerResolver {
	return func(wikiPath string) string {
		r := ""
		if base != nil {
			r = strings.TrimSpace(base(wikiPath))
		}
		if (r == "" || r == operations.ReviewerFallback) && b.hasMember(LibrarianSlug) {
			return LibrarianSlug
		}
		return r
	}
}

// librarianWikiAuthorityBlock is the prompt block emitted for the Librarian in
// place of the generic specialist wiki rules. It encodes the Phase 4 authority
// move: the Librarian curates and reviews notebook→wiki promotions (the CEO no
// longer does). Numbering continues the specialist rules (the caller appends a
// "13. …stop" rule after this block).
func librarianWikiAuthorityBlock() string {
	return "== WIKI OWNERSHIP (you are the Librarian) ==\n" +
		"You own the team's wiki — keep it accurate, well-organized, and easy to search — and you are the reviewer for notebook→wiki promotions. Owning agents write their own notebooks; you curate, format, organize, and decide what becomes canonical. You do not author other agents' notes for them.\n" +
		"12. Use team_notebook_review to see which notebook entries have earned promotion demand (cross-agent searches, channel context-asks), ranked by convergence. Review the high-demand ones and flag entries worth promoting. Calling team_notebook_review is itself a demand signal — use it when you are actually curating, not as background polling.\n" +
		"12b. Before writing or accepting knowledge as canonical, use wuphf_wiki_lookup / team_wiki_search / team_wiki_list / notebook_search to see what already exists, so articles get merged and cross-linked instead of duplicated.\n" +
		"12c. You may write the canonical wiki DIRECTLY with team_wiki_write (mode create / replace / append_section) — you do not need a human request or a promotion to format, restructure, fix broken links, or land curated knowledge, because the wiki is your responsibility. Keep clear titles and sections, and keep `scratch: true` working notes out of the canonical wiki. (Other agents still draft in notebooks and promote for your review; the direct-write path is yours.)\n" +
		"12d. When the CEO, another agent, or the human asks to preserve something for the team, make sure it lands: either promote the relevant notebook entry or write the article yourself, then confirm it is well-formed and linked.\n"
}
