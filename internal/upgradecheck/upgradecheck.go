// Package upgradecheck compares the running wuphf version against the latest
// published on npm and (optionally) summarises the diff via the GitHub
// compare API. It is consumed by the `wuphf upgrade` subcommand and by the
// in-app web banner.
//
// Scope note: the npm shim (npm/bin/wuphf.js, PR #273) already self-heals
// when the installed wuphf binary falls behind the latest published release,
// printing its own one-line stderr hint. This package intentionally does
// NOT add a second startup notice — it only powers explicit user-driven
// surfaces (`wuphf upgrade`, the web banner).
package upgradecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/buildinfo"
)

const (
	NPMPackage     = "wuphf"
	GitHubRepo     = "nex-crm/wuphf"
	NPMRegistryURL = "https://registry.npmjs.org/" + NPMPackage + "/latest"
)

// Result reports the comparison between the running version and the latest
// published version.
type Result struct {
	Current          string `json:"current"`
	Latest           string `json:"latest"`
	UpgradeAvailable bool   `json:"upgrade_available"`
	CompareURL       string `json:"compare_url"`
	UpgradeCommand   string `json:"upgrade_command"`
}

// Check fetches the latest version from npm and compares it to the running
// build. It always returns a Result with Current populated; Latest is empty
// when the registry call fails.
func Check(ctx context.Context, client *http.Client) (Result, error) {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	current := buildinfo.Current().Version
	res := Result{
		Current:        current,
		UpgradeCommand: "npm install -g " + NPMPackage + "@latest",
	}

	latest, err := fetchLatestVersion(ctx, client)
	if err != nil {
		return res, err
	}
	res.Latest = latest
	res.UpgradeAvailable = compareVersions(current, latest) < 0
	res.CompareURL = fmt.Sprintf(
		"https://github.com/%s/compare/v%s...v%s",
		GitHubRepo,
		strings.TrimPrefix(current, "v"),
		strings.TrimPrefix(latest, "v"),
	)
	return res, nil
}

// ── npm registry ──────────────────────────────────────────────────────────

type npmManifest struct {
	Version string `json:"version"`
}

func fetchLatestVersion(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NPMRegistryURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wuphf-upgradecheck/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var m npmManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	v := strings.TrimSpace(m.Version)
	if v == "" {
		return "", errors.New("npm manifest missing version")
	}
	return v, nil
}

// ── GitHub compare ────────────────────────────────────────────────────────

// CommitEntry is one parsed conventional-commit entry from a GitHub compare
// response.
type CommitEntry struct {
	Type        string
	Scope       string
	Description string
	PR          string
	SHA         string
	Breaking    bool
}

type githubCompareCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

type githubCompareResponse struct {
	Commits []githubCompareCommit `json:"commits"`
}

// FetchChangelog calls the GitHub compare API and parses the commit list into
// conventional-commit entries.
func FetchChangelog(ctx context.Context, client *http.Client, from, to string) ([]CommitEntry, error) {
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second}
	}
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/compare/v%s...v%s",
		GitHubRepo,
		strings.TrimPrefix(from, "v"),
		strings.TrimPrefix(to, "v"),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wuphf-upgradecheck/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github compare status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if err != nil {
		return nil, err
	}
	var data githubCompareResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	out := make([]CommitEntry, 0, len(data.Commits))
	for _, c := range data.Commits {
		out = append(out, parseCommit(c.Commit.Message, c.SHA))
	}
	return out, nil
}

var (
	// Capture groups: 1=type, 2=(scope), 3=! (breaking), 4=description.
	conventionalRE = regexp.MustCompile(`^(?i)(feat|fix|perf|refactor|docs|chore|test|build|ci|style|revert)(\([^)]+\))?(!)?:\s*(.+?)\s*$`)
	// Trailing PR ref e.g. " (#310)". Anchored to end-of-string so an inline
	// "(#42)" inside the description is preserved as text and only the
	// terminating reference is treated as the linked PR (matches the TS
	// parser so the CLI and web banner render identically).
	trailingPRRE = regexp.MustCompile(`\s*\(#(\d+)\)\s*$`)
)

func parseCommit(message, sha string) CommitEntry {
	subject := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
	m := conventionalRE.FindStringSubmatch(subject)
	if m == nil {
		return CommitEntry{Type: "other", Description: subject, PR: extractTrailingPR(subject), SHA: sha}
	}
	scope := strings.Trim(m[2], "()")
	breaking := m[3] == "!"
	rest := strings.TrimSpace(m[4])
	pr := extractTrailingPR(rest)
	desc := strings.TrimSpace(trailingPRRE.ReplaceAllString(rest, ""))
	return CommitEntry{
		Type:        strings.ToLower(m[1]),
		Scope:       scope,
		Description: desc,
		PR:          pr,
		SHA:         sha,
		Breaking:    breaking,
	}
}

func extractTrailingPR(s string) string {
	m := trailingPRRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// FormatChangelog renders entries grouped by conventional-commit type. Useful
// for the `wuphf upgrade` subcommand. Breaking-change commits are surfaced
// in their own group at the top regardless of their underlying type.
func FormatChangelog(entries []CommitEntry) string {
	if len(entries) == 0 {
		return "  (no commits found)\n"
	}
	type group struct {
		key, label string
	}
	order := []group{
		{"breaking", "Breaking changes"},
		{"feat", "New features"},
		{"fix", "Bug fixes"},
		{"perf", "Performance"},
		{"refactor", "Refactoring"},
		{"docs", "Documentation"},
		{"other", "Other changes"},
	}
	known := map[string]bool{}
	for _, g := range order {
		known[g.key] = true
	}
	buckets := map[string][]CommitEntry{}
	for _, e := range entries {
		if e.Breaking {
			buckets["breaking"] = append(buckets["breaking"], e)
			continue
		}
		k := e.Type
		if !known[k] {
			k = "other"
		}
		buckets[k] = append(buckets[k], e)
	}
	var b strings.Builder
	for _, g := range order {
		es := buckets[g.key]
		if len(es) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s\n", g.label)
		sort.SliceStable(es, func(i, j int) bool { return es[i].Scope < es[j].Scope })
		for _, e := range es {
			scope := ""
			if e.Scope != "" {
				scope = "[" + e.Scope + "] "
			}
			pr := ""
			if e.PR != "" {
				pr = "  (#" + e.PR + ")"
			}
			fmt.Fprintf(&b, "    • %s%s%s\n", scope, e.Description, pr)
		}
	}
	return b.String()
}

// ── Version comparison ────────────────────────────────────────────────────

// compareVersions compares dotted-numeric versions like "0.79.10". Returns
// -1 if a < b, 0 if equal, 1 if a > b. Non-numeric segments collapse to 0.
//
// Pre-release suffixes (e.g. "0.79.10-rc.1") are not interpreted; the
// numeric tail is parsed up to the first non-numeric character, so
// "0.79.10-rc.1" sorts equal to "0.79.10". Acceptable here because npm's
// `latest` dist-tag is conventionally a stable release. If we ever publish
// pre-releases under `latest`, swap in a real semver comparator.
func compareVersions(a, b string) int {
	pa := splitVersion(a)
	pb := splitVersion(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		x := segAt(pa, i)
		y := segAt(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out[i] = n
	}
	return out
}

func segAt(parts []int, i int) int {
	if i >= len(parts) {
		return 0
	}
	return parts[i]
}
