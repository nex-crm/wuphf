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
	NPMPackage = "wuphf"
	GitHubRepo = "nex-crm/wuphf"
)

// npmRegistryURL is a var (not a const) so tests can swap in a httptest
// server URL directly without the RoundTripper indirection the original
// fixture used. Unexported so cross-package callers can't mutate it.
//
// Caveat: tests that swap this MUST NOT call t.Parallel() — concurrent
// reads in fetchLatestVersion against a write in pinNPMRegistryURL trip
// the race detector. The serial-by-default usage here is fine; if a
// future test wants parallel execution, thread the URL through Check
// instead of swapping the package var.
var npmRegistryURL = "https://registry.npmjs.org/" + NPMPackage + "/latest"

// Result reports the comparison between the running version and the latest
// published version.
type Result struct {
	Current          string `json:"current"`
	Latest           string `json:"latest"`
	UpgradeAvailable bool   `json:"upgrade_available"`
	// IsDevBuild is true when Current is the buildinfo "dev" sentinel — i.e.
	// the binary was compiled from source without a release ldflag. Callers
	// MUST treat UpgradeAvailable as meaningless in that case (the comparison
	// `dev < anything` is true but the user did not install via npm and the
	// upgrade command is wrong for them).
	IsDevBuild     bool   `json:"is_dev_build"`
	CompareURL     string `json:"compare_url,omitempty"`
	UpgradeCommand string `json:"upgrade_command"`
}

// Check fetches the latest version from npm and compares it to the running
// build. It always returns a Result with Current populated; Latest is empty
// when the registry call fails.
func Check(ctx context.Context, client *http.Client) (Result, error) {
	if client == nil {
		// Generous outer cap so a future caller passing
		// context.Background() can't hang forever on a half-open
		// socket. The 30s ceiling is well above any realistic caller
		// budget (broker 5s, CLI 8s) so it never overrules the
		// caller-supplied context.
		client = &http.Client{Timeout: 30 * time.Second}
	}
	current := currentVersion()
	res := Result{
		Current:        current,
		IsDevBuild:     buildinfo.IsDev(current),
		UpgradeCommand: "npm install -g " + NPMPackage + "@latest",
	}

	latest, err := fetchLatestVersion(ctx, client)
	if err != nil {
		return res, err
	}
	res.Latest = latest
	// A dev build's "version" is the literal string "dev" — comparing it
	// numerically against npm's `latest` would always say "upgrade
	// available" and tell the user to `npm install -g`, which would
	// blindly replace their source build. Bail out cleanly instead;
	// callers render the dev-build branch off IsDevBuild.
	if res.IsDevBuild {
		return res, nil
	}
	res.UpgradeAvailable = compareVersions(current, latest) < 0
	if res.UpgradeAvailable {
		res.CompareURL = fmt.Sprintf(
			"https://github.com/%s/compare/v%s...v%s",
			GitHubRepo,
			strings.TrimPrefix(current, "v"),
			strings.TrimPrefix(latest, "v"),
		)
	}
	return res, nil
}

// currentVersion is a seam so tests can pin the running version without
// mutating buildinfo or rebuilding. Production reads buildinfo.Current()
// directly; tests overwrite this var via t.Cleanup-restored swaps.
var currentVersion = func() string { return buildinfo.Current().Version }

// ── npm registry ──────────────────────────────────────────────────────────

type npmManifest struct {
	Version string `json:"version"`
}

func fetchLatestVersion(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, npmRegistryURL, nil)
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
// response. JSON tags MUST stay lowercase to match the TS shape consumed by
// web/src/components/layout/UpgradeBanner.tsx — without them, the broker
// would ship "Type"/"Scope"/etc. and the banner's c.type lookup would
// silently return undefined for every entry.
type CommitEntry struct {
	Type        string `json:"type"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
	PR          string `json:"pr"`
	SHA         string `json:"sha"`
	Breaking    bool   `json:"breaking"`
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
	// Defense-in-depth: validate from/to here too, not just at the
	// broker handler. Today both call sites are safe (broker validates
	// via upgradeVersionParam, CLI feeds buildinfo + npm registry
	// values), but a future caller reaching this package directly
	// must NOT be able to ship `..` / `/` / `@host` segments to the
	// upstream compare URL.
	if !VersionParamRE.MatchString(from) || !VersionParamRE.MatchString(to) {
		return nil, fmt.Errorf("upgradecheck: invalid version params from=%q to=%q", from, to)
	}
	if client == nil {
		// See note in Check: same generous outer cap as a safety net
		// for callers that forget to set a context deadline.
		client = &http.Client{Timeout: 30 * time.Second}
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

// VersionParamRE accepts an optional `v`, 2-4 dotted numeric segments, an
// optional pre-release suffix after `-`, and an optional `+build.5` build
// metadata. MIRRORED in internal/team/broker.go (upgradeVersionParam) and
// web/src/components/layout/upgradeBanner.utils.ts (VERSION_RE) — keep all
// three in sync so a future tightening doesn't drift between them.
var VersionParamRE = regexp.MustCompile(`^v?\d+(\.\d+){1,3}(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

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
// Pre-release suffixes (e.g. "0.79.10-rc.1") are stripped before comparison
// so "0.79.10-rc.1" sorts equal to "0.79.10". This is intentionally NOT a
// full semver comparator — npm's `latest` dist-tag is conventionally a
// stable release and we only need ordering within the stable line. If we
// ever publish pre-releases under `latest`, swap in a real semver lib.
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
	// Drop pre-release suffix (`-rc.1`) AND build metadata (`+build.5`)
	// so neither poisons the dotted segments (Atoi on "10-rc" returns
	// 0, which would order an rc release BELOW its prior stable —
	// exactly opposite of intent). Mirror in the TS twin.
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
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
