package channelui

// OfficeMember describes a member of the office roster as the broker
// returns it. The shape mirrors the broker's JSON contract and matches
// the official manifest spec.
type OfficeMember struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Expertise   []string `json:"expertise,omitempty"`
	Personality string   `json:"personality,omitempty"`
	BuiltIn     bool     `json:"built_in,omitempty"`
}

// officeDirectory is the process-wide office roster used by DisplayName
// and RoleLabel to resolve human-friendly names. It is a singleton
// because the channel UI assumes one workspace at a time; callers refresh
// it via SetOfficeDirectory whenever a fresh roster arrives from the
// broker.
//
// Tests that need to inject a fixture should call WithOfficeDirectoryForTest
// (which captures the prior state and registers a t.Cleanup that restores
// it). Tests that mutate the directory through SetOfficeDirectory directly
// bypass that isolation and will leak fixture state into sibling tests.
var officeDirectory = map[string]OfficeMember{}

// SetOfficeDirectory replaces the singleton directory. Idempotent;
// safe to call repeatedly with the same input.
func SetOfficeDirectory(members []OfficeMember) {
	dir := make(map[string]OfficeMember, len(members))
	for _, member := range members {
		dir[member.Slug] = member
	}
	officeDirectory = dir
}

// WithOfficeDirectoryForTest installs members as the directory for the
// duration of the test, then restores the previous contents via a
// t.Cleanup. The helper takes testing.TB rather than *testing.T so it
// works inside benchmarks and table subtests without changing call
// shape.
func WithOfficeDirectoryForTest(t interface {
	Cleanup(func())
	Helper()
}, members []OfficeMember) {
	t.Helper()
	prior := officeDirectory
	SetOfficeDirectory(members)
	t.Cleanup(func() { officeDirectory = prior })
}

// LookupMember returns the office member registered under slug, or the
// zero value and false. Useful for callers that want raw access to the
// roster without going through DisplayName/RoleLabel formatting.
func LookupMember(slug string) (OfficeMember, bool) {
	m, ok := officeDirectory[slug]
	return m, ok
}

// DisplayName resolves a human-readable label for an agent slug.
// Custom names from the office directory take precedence; otherwise we
// fall back to the canonical title for one of the built-in roles, and
// finally to "@<slug>" for unknown agents.
func DisplayName(slug string) string {
	if member, ok := officeDirectory[slug]; ok && member.Name != "" {
		return member.Name
	}
	switch slug {
	case "ceo":
		return "CEO"
	case "pm":
		return "Product Manager"
	case "fe":
		return "Frontend Engineer"
	case "be":
		return "Backend Engineer"
	case "ai":
		return "AI Engineer"
	case "designer":
		return "Designer"
	case "cmo":
		return "CMO"
	case "cro":
		return "CRO"
	case "nex":
		return "Nex"
	case "you":
		return "You"
	default:
		return "@" + slug
	}
}

// RoleLabel resolves a short role descriptor for an agent slug, used in
// secondary metadata lines (e.g., "frontend" under the FE avatar).
// Falls back to the canonical role for built-in slugs, then to
// "teammate" for anything else.
func RoleLabel(slug string) string {
	if member, ok := officeDirectory[slug]; ok && member.Role != "" {
		return member.Role
	}
	switch slug {
	case "ceo":
		return "strategy"
	case "pm":
		return "product"
	case "fe":
		return "frontend"
	case "be":
		return "backend"
	case "ai":
		return "AI Engineer"
	case "designer":
		return "design"
	case "cmo":
		return "marketing"
	case "cro":
		return "revenue"
	case "nex":
		return "context graph"
	case "you":
		return "human"
	default:
		return "teammate"
	}
}
