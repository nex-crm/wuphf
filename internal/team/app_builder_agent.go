package team

import (
	"strings"
	"time"
)

// app_builder_agent.go — the App Builder is a first-class, always-present agent
// (slug "app-builder", display name "App Builder") that turns repeatable
// workflows into Apps (internal tools). Like the CEO and the Librarian it is
// BUILT-IN: present in every new workspace regardless of blueprint or selected
// agents, and back-filled onto existing rosters on load.
//
// The roster-seed chokepoints live in broker_defaults.go: defaultOfficeMembers
// (fresh offices) and normalizeLoadedStateLocked (persisted offices loaded from
// disk that predate the agent). Both call ensureAppBuilderOfficeMember so the
// App Builder shows up in the sidebar and can own App-build tasks. This mirrors
// the Librarian rollout (librarian.go) exactly so the two stay consistent.
//
// appBuilderSlug itself is defined in broker_apps_proposal.go (the proposal
// path needed it first); persona/expertise come from the shared inference in
// broker_member_construction.go so the strings live in one place.

const appBuilderRole = "App Builder"

// isAppBuilderSlug reports whether slug is the App Builder (case-insensitive).
func isAppBuilderSlug(slug string) bool {
	return strings.EqualFold(strings.TrimSpace(slug), appBuilderSlug)
}

// appBuilderOfficeMember builds the App Builder's officeMember record. Expertise
// and personality reuse the shared inference (broker_member_construction.go) so
// the back-filled member matches one constructed from the manifest spec.
func appBuilderOfficeMember(createdAt string) officeMember {
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	return officeMember{
		Slug:        appBuilderSlug,
		Name:        appBuilderRole,
		Role:        appBuilderRole,
		Expertise:   inferOfficeExpertise(appBuilderSlug, appBuilderRole),
		Personality: inferOfficePersonality(appBuilderSlug, appBuilderRole),
		BuiltIn:     true,
		CreatedBy:   "wuphf",
		CreatedAt:   createdAt,
	}
}

// ensureAppBuilderOfficeMember returns members with the App Builder present,
// appending it when absent (matched case-insensitively by slug). Used at every
// roster-seed chokepoint so the App Builder is always in the office, like the
// CEO and the Librarian.
func ensureAppBuilderOfficeMember(members []officeMember) []officeMember {
	for i := range members {
		if isAppBuilderSlug(members[i].Slug) {
			return members
		}
	}
	return append(members, appBuilderOfficeMember(""))
}
