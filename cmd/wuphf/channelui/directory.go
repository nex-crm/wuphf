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
// Tests can override the directory by calling SetOfficeDirectory with a
// fixture and resetting via t.Cleanup.
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
