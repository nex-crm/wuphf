package company

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

type MemberSpec struct {
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Role           string   `json:"role,omitempty"`
	Expertise      []string `json:"expertise,omitempty"`
	Personality    string   `json:"personality,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
	System         bool     `json:"system,omitempty"`
}

type ChannelSpec struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
	Disabled    []string `json:"disabled,omitempty"`
}

type Manifest struct {
	Name        string        `json:"name,omitempty"`
	Description string        `json:"description,omitempty"`
	Lead        string        `json:"lead,omitempty"`
	Members     []MemberSpec  `json:"members,omitempty"`
	Channels    []ChannelSpec `json:"channels,omitempty"`
	UpdatedAt   string        `json:"updated_at,omitempty"`
}

func ManifestPath() string {
	if path := strings.TrimSpace(os.Getenv("WUPHF_COMPANY_FILE")); path != "" {
		return path
	}
	if path := strings.TrimSpace(os.Getenv("NEX_COMPANY_FILE")); path != "" {
		return path
	}

	if cwd, err := os.Getwd(); err == nil {
		local := filepath.Join(cwd, "wuphf.company.json")
		if _, err := os.Stat(local); err == nil {
			return local
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "company.json")
	}
	return filepath.Join(home, ".wuphf", "company.json")
}

func LoadManifest() (Manifest, error) {
	path := ManifestPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			manifest := DefaultManifest()
			return manifest, nil
		}
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	manifest = normalizeManifest(manifest)
	return manifest, nil
}

func SaveManifest(manifest Manifest) error {
	manifest = normalizeManifest(manifest)
	manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	path := ManifestPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func DefaultManifest() Manifest {
	now := time.Now().UTC().Format(time.RFC3339)
	pack := agent.GetPack("founding-team")
	manifest := Manifest{
		Name:        "The WUPHF Office",
		Description: "Autonomous office runtime for the founding team.",
		Lead:        "ceo",
		UpdatedAt:   now,
	}
	if pack == nil {
		manifest.Members = []MemberSpec{
			{Slug: "ceo", Name: "CEO", Role: "CEO", System: true},
			{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
			{Slug: "fe", Name: "Frontend Engineer", Role: "Frontend Engineer"},
			{Slug: "be", Name: "Backend Engineer", Role: "Backend Engineer"},
			{Slug: "ai", Name: "AI Engineer", Role: "AI Engineer"},
			{Slug: "designer", Name: "Designer", Role: "Designer"},
			{Slug: "cmo", Name: "CMO", Role: "CMO"},
			{Slug: "cro", Name: "CRO", Role: "CRO"},
		}
	} else {
		for _, cfg := range pack.Agents {
			manifest.Members = append(manifest.Members, MemberSpec{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				System:         cfg.Slug == "ceo",
			})
		}
	}
	generalMembers := make([]string, 0, len(manifest.Members))
	for _, member := range manifest.Members {
		generalMembers = append(generalMembers, member.Slug)
	}
	manifest.Channels = []ChannelSpec{{
		Slug:        "general",
		Name:        "general",
		Description: "The default company-wide room for top-level coordination, announcements, and cross-functional discussion.",
		Members:     generalMembers,
	}}
	return normalizeManifest(manifest)
}

func normalizeManifest(manifest Manifest) Manifest {
	if strings.TrimSpace(manifest.Name) == "" {
		manifest.Name = "The WUPHF Office"
	}
	if strings.TrimSpace(manifest.Lead) == "" {
		manifest.Lead = "ceo"
	}

	seenMembers := make(map[string]struct{}, len(manifest.Members))
	members := make([]MemberSpec, 0, len(manifest.Members))
	for _, member := range manifest.Members {
		member.Slug = normalizeSlug(member.Slug)
		if member.Slug == "" {
			continue
		}
		if _, ok := seenMembers[member.Slug]; ok {
			continue
		}
		seenMembers[member.Slug] = struct{}{}
		member.Name = strings.TrimSpace(member.Name)
		if member.Name == "" {
			member.Name = humanizeSlug(member.Slug)
		}
		member.Role = strings.TrimSpace(member.Role)
		if member.Role == "" {
			member.Role = member.Name
		}
		member.Expertise = normalizeStrings(member.Expertise)
		member.AllowedTools = normalizeStrings(member.AllowedTools)
		member.System = member.Slug == manifest.Lead || member.Slug == "ceo" || member.System
		members = append(members, member)
	}
	if len(members) == 0 {
		return DefaultManifest()
	}
	manifest.Members = members

	seenChannels := make(map[string]struct{}, len(manifest.Channels))
	channels := make([]ChannelSpec, 0, len(manifest.Channels))
	for _, channel := range manifest.Channels {
		channel.Slug = normalizeSlug(channel.Slug)
		if channel.Slug == "" {
			continue
		}
		if _, ok := seenChannels[channel.Slug]; ok {
			continue
		}
		seenChannels[channel.Slug] = struct{}{}
		channel.Name = strings.TrimSpace(channel.Name)
		if channel.Name == "" {
			channel.Name = channel.Slug
		}
		channel.Description = strings.TrimSpace(channel.Description)
		if channel.Description == "" {
			channel.Description = defaultChannelDescription(channel.Slug, channel.Name)
		}
		channel.Members = normalizeSlugs(channel.Members)
		channel.Disabled = normalizeSlugs(channel.Disabled)
		channel.Members = ensureLeadMember(channel.Members, manifest.Lead)
		channel.Disabled = removeSlug(channel.Disabled, manifest.Lead)
		channels = append(channels, channel)
	}
	if !containsChannel(channels, "general") {
		members := make([]string, 0, len(manifest.Members))
		for _, member := range manifest.Members {
			members = append(members, member.Slug)
		}
		channels = append([]ChannelSpec{{
			Slug:        "general",
			Name:        "general",
			Description: defaultChannelDescription("general", "general"),
			Members:     ensureLeadMember(members, manifest.Lead),
		}}, channels...)
	}
	manifest.Channels = channels
	return manifest
}

func containsChannel(channels []ChannelSpec, slug string) bool {
	for _, channel := range channels {
		if channel.Slug == slug {
			return true
		}
	}
	return false
}

func defaultChannelDescription(slug, name string) string {
	if strings.TrimSpace(slug) == "" {
		slug = strings.TrimSpace(name)
	}
	switch normalizeSlug(slug) {
	case "general":
		return "The default company-wide room for top-level coordination, announcements, and cross-functional discussion."
	default:
		label := strings.TrimSpace(name)
		if label == "" {
			label = humanizeSlug(slug)
		}
		return label + " focused work. Use this channel for discussion, decisions, and execution specific to that stream."
	}
}

func ensureLeadMember(members []string, lead string) []string {
	lead = normalizeSlug(lead)
	if lead == "" {
		lead = "ceo"
	}
	if containsSlug(members, lead) {
		return normalizeSlugs(members)
	}
	return append([]string{lead}, normalizeSlugs(members)...)
}

func removeSlug(items []string, slug string) []string {
	slug = normalizeSlug(slug)
	var out []string
	for _, item := range normalizeSlugs(items) {
		if item != slug {
			out = append(out, item)
		}
	}
	return out
}

func containsSlug(items []string, want string) bool {
	want = normalizeSlug(want)
	for _, item := range normalizeSlugs(items) {
		if item == want {
			return true
		}
	}
	return false
}

func normalizeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeSlugs(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = normalizeSlug(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeSlug(input string) string {
	slug := strings.ToLower(strings.TrimSpace(input))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	return slug
}

func humanizeSlug(slug string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
