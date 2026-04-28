// Package skillpublish carries the pure logic for turning a compiled WUPHF
// skill into a publishable manifest for one of the public agent-skill hubs
// (anthropics/skills, claude-marketplace, lobehub, or any github:owner/repo
// fork).
//
// All identifiers and URL templates live in this package so adding a new hub
// is a single map entry plus one switch arm — no CLI plumbing required.
//
// The package is intentionally side-effect-free: actual PR creation is shelled
// out to `gh` from the cmd/wuphf layer so we never roll our own GitHub API
// client.
package skillpublish

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Hub identifies a destination registry. Hub keys are kept short and
// dash-free so they read well as `--to anthropics`, `--to lobehub`, etc.
type Hub string

const (
	HubAnthropics        Hub = "anthropics"
	HubClaudeMarketplace Hub = "claude-marketplace"
	HubLobeHub           Hub = "lobehub"
	// HubGitHubPrefix marks a one-off `github:owner/repo` target. The full
	// hub string keeps the prefix attached so callers can pass it through
	// unmodified — `IsGitHubScheme` is the canonical detector.
	HubGitHubPrefix = "github:"
)

// KnownHubs is the closed-set of hub keys we ship out of the box. The
// `github:` scheme is open-ended and intentionally NOT listed here; callers
// detect it via IsGitHubScheme.
var KnownHubs = []Hub{HubAnthropics, HubClaudeMarketplace, HubLobeHub}

// IsGitHubScheme reports whether the hub string is a `github:owner/repo`
// target rather than a named registry.
func IsGitHubScheme(hub string) bool {
	return strings.HasPrefix(strings.TrimSpace(hub), HubGitHubPrefix)
}

// ParseGitHubScheme splits a `github:owner/repo` hub string into (owner, repo).
// Returns an error when the scheme is malformed.
func ParseGitHubScheme(hub string) (owner, repo string, err error) {
	hub = strings.TrimSpace(hub)
	if !IsGitHubScheme(hub) {
		return "", "", fmt.Errorf("skillpublish: not a github: scheme: %q", hub)
	}
	body := strings.TrimPrefix(hub, HubGitHubPrefix)
	parts := strings.SplitN(body, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("skillpublish: github scheme must be github:owner/repo, got %q", hub)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

// Manifest is the wire-shape WUPHF emits per published skill. Frontmatter
// fields (Name/Description/Version/License) come straight from the SKILL.md;
// Body holds the markdown body verbatim; Source is a stable provenance string
// of the form `wuphf-{repo}-{slug}`; PublishedAt is RFC3339 UTC.
//
// JSON tags are kept stable: the manifest is the durable contract a hub
// indexer can rely on if/when WUPHF adds a hub-side companion service.
type Manifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	License     string `json:"license"`
	Body        string `json:"body"`
	Source      string `json:"source"`
	PublishedAt string `json:"published_at"`
}

// FrontmatterLike is the minimal shape BuildManifest needs; using a small
// local interface keeps internal/skillpublish independent of internal/team
// (which would otherwise create an import cycle once team starts consuming
// publish helpers).
type FrontmatterLike struct {
	Name        string
	Description string
	Version     string
	License     string
}

// BuildManifest assembles a Manifest from frontmatter + body. License
// defaults to MIT when blank; Version defaults to 1.0.0 when blank.
//
// Both defaults match the values RenderSkillMarkdown writes when a skill is
// first compiled, so a round-trip through the file system never changes the
// shape of the published manifest.
//
// Source format `wuphf-{repo}-{slug}` is the provenance string a hub indexer
// can use to dedupe — passing repo="" yields `wuphf-{slug}` so the prefix is
// still recognisable when no workspace is set.
func BuildManifest(fm FrontmatterLike, body string, repo string, publishedAt time.Time) (Manifest, error) {
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		return Manifest{}, errors.New("skillpublish: frontmatter name is required")
	}
	desc := strings.TrimSpace(fm.Description)
	if desc == "" {
		return Manifest{}, errors.New("skillpublish: frontmatter description is required")
	}
	version := strings.TrimSpace(fm.Version)
	if version == "" {
		version = "1.0.0"
	}
	license := strings.TrimSpace(fm.License)
	if license == "" {
		license = "MIT"
	}
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}
	source := buildSource(repo, name)
	return Manifest{
		Name:        name,
		Description: desc,
		Version:     version,
		License:     license,
		Body:        strings.TrimSpace(body),
		Source:      source,
		PublishedAt: publishedAt.UTC().Format(time.RFC3339),
	}, nil
}

func buildSource(repo, slug string) string {
	repo = sanitizeSourceSegment(repo)
	slug = sanitizeSourceSegment(slug)
	if repo == "" {
		return "wuphf-" + slug
	}
	return "wuphf-" + repo + "-" + slug
}

// sanitizeSourceSegment lower-cases and strips characters that don't belong
// in a hub-side identifier. The result is `[a-z0-9-]+`. Empty input stays
// empty so callers can detect it.
func sanitizeSourceSegment(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	var out strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-' || r == '_':
			out.WriteRune('-')
		case r == ' ' || r == '/':
			out.WriteRune('-')
		}
	}
	return strings.Trim(out.String(), "-")
}

// HubURL returns the canonical raw-content base URL for a published skill on
// the named hub. For the `github:owner/repo` scheme, this returns the raw
// URL pointing at the default-branch (`main`) copy of the SKILL.md.
//
// The returned URL is suitable for `wuphf skills install` — drop in `name`
// and the install path will fetch a public, unauthenticated raw markdown
// blob from GitHub.
func HubURL(hub, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("skillpublish: name is required")
	}
	if IsGitHubScheme(hub) {
		owner, repo, err := ParseGitHubScheme(hub)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"https://raw.githubusercontent.com/%s/%s/main/skills/%s/SKILL.md",
			url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(name),
		), nil
	}
	switch Hub(strings.TrimSpace(hub)) {
	case HubAnthropics:
		return fmt.Sprintf(
			"https://raw.githubusercontent.com/anthropics/skills/main/skills/%s/SKILL.md",
			url.PathEscape(name),
		), nil
	case HubClaudeMarketplace:
		return fmt.Sprintf(
			"https://raw.githubusercontent.com/claude-marketplace/skills/main/skills/%s/SKILL.md",
			url.PathEscape(name),
		), nil
	case HubLobeHub:
		// LobeHub's community index uses a flat agents/{name}.md layout;
		// callers fetch the raw file the same way.
		return fmt.Sprintf(
			"https://raw.githubusercontent.com/lobehub/lobe-chat-agents/main/agents/%s.md",
			url.PathEscape(name),
		), nil
	default:
		return "", fmt.Errorf("skillpublish: unknown hub %q (known: anthropics, claude-marketplace, lobehub, github:owner/repo)", hub)
	}
}

// HubFilePath returns the path within the hub repo where the skill should be
// written by the publish PR. For most hubs this is `skills/{name}/SKILL.md`;
// LobeHub uses the flat `agents/{name}.md` layout.
func HubFilePath(hub, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("skillpublish: name is required")
	}
	if IsGitHubScheme(hub) {
		// Custom github targets default to the conventional layout. Callers
		// can override at the CLI layer if they want a different shape.
		return fmt.Sprintf("skills/%s/SKILL.md", name), nil
	}
	switch Hub(strings.TrimSpace(hub)) {
	case HubAnthropics, HubClaudeMarketplace:
		return fmt.Sprintf("skills/%s/SKILL.md", name), nil
	case HubLobeHub:
		return fmt.Sprintf("agents/%s.md", name), nil
	default:
		return "", fmt.Errorf("skillpublish: unknown hub %q (known: anthropics, claude-marketplace, lobehub, github:owner/repo)", hub)
	}
}

// HubRepo returns the `owner/repo` slug for the hub. Used by `gh pr create`
// and PR-URL templates. github:owner/repo schemes round-trip unchanged.
func HubRepo(hub string) (string, error) {
	if IsGitHubScheme(hub) {
		owner, repo, err := ParseGitHubScheme(hub)
		if err != nil {
			return "", err
		}
		return owner + "/" + repo, nil
	}
	switch Hub(strings.TrimSpace(hub)) {
	case HubAnthropics:
		return "anthropics/skills", nil
	case HubClaudeMarketplace:
		return "claude-marketplace/skills", nil
	case HubLobeHub:
		return "lobehub/lobe-chat-agents", nil
	default:
		return "", fmt.Errorf("skillpublish: unknown hub %q (known: anthropics, claude-marketplace, lobehub, github:owner/repo)", hub)
	}
}

// HubBaseBranch returns the base branch the publish PR should target.
// All known hubs target `main` today; kept as its own helper so a future
// hub with a non-main default doesn't require touching the CLI.
func HubBaseBranch(hub string) string {
	return "main"
}

// PublishBranchName builds a stable branch name used for the publish PR.
// Format: `wuphf-publish-{slug}-{unix-timestamp}`. The unix timestamp keeps
// branches collision-free across repeated publishes.
func PublishBranchName(slug string, t time.Time) string {
	slug = sanitizeSourceSegment(slug)
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return fmt.Sprintf("wuphf-publish-%s-%d", slug, t.UTC().Unix())
}
