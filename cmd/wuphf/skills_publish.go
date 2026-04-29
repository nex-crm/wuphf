package main

// skills_publish.go wires the public-hub publish/install loop:
//
//   wuphf skills publish <slug-or-path> --to <hub>     (export)
//   wuphf skills install <name>          --from <hub>  (import)
//
// Publish shells out to `gh` for actual PR creation so we never roll our own
// GitHub API client. Install is a public raw-fetch + broker POST round-trip.
//
// All hub URL/path knowledge lives in internal/skillpublish; this file owns
// argument parsing, file IO, gh invocations, and HTTP plumbing.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/skillpublish"
	"github.com/nex-crm/wuphf/internal/team"
)

// skillsHTTPTimeout caps every external HTTP call (GitHub raw fetch + broker
// POST) so a network blip never wedges the CLI. Generous enough that a slow
// home network still works.
const skillsHTTPTimeout = 30 * time.Second

// ghCommandTimeout caps every `gh` subprocess. PR creation against GitHub
// takes a couple of seconds in steady state; 60s leaves headroom for rebases
// and credential prompts.
const ghCommandTimeout = 60 * time.Second

// runSkillsCmd is the dispatcher for the `wuphf skills <verb>` subcommand
// family. Verbs are kept thin so each handler can own its own flag set.
//
// Help routing: bare `wuphf skills` and `wuphf skills --help` print the
// family-level usage; per-verb help (`wuphf skills publish --help`) is
// handled inside each verb's own FlagSet so users see flag-level docs.
func runSkillsCmd(args []string) {
	if len(args) == 0 {
		printSkillsHelp()
		os.Exit(2)
	}
	if isFamilyHelp(args[0]) {
		printSkillsHelp()
		return
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "publish":
		runSkillsPublish(rest)
	case "install":
		runSkillsInstall(rest)
	default:
		fmt.Fprintf(os.Stderr, "wuphf skills: unknown verb %q — run `wuphf skills --help` for the list.\n", verb)
		os.Exit(2)
	}
}

// isFamilyHelp reports whether the first arg is a family-level help flag
// (so `wuphf skills --help` prints the verb table, but `wuphf skills publish
// --help` falls through to publish's own flag-set).
func isFamilyHelp(first string) bool {
	switch first {
	case "-h", "--help", "-help", "help":
		return true
	}
	return false
}

func printSkillsHelp() {
	fmt.Fprintln(os.Stderr, "wuphf skills — publish/install team skills against public hubs")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  wuphf skills publish <slug-or-path> --to <hub>     Open a PR with this skill")
	fmt.Fprintln(os.Stderr, "  wuphf skills install <name> --from <hub>           Pull a skill into your wiki")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Hubs: anthropics, claude-marketplace, lobehub, github:owner/repo")
}

// ── publish ────────────────────────────────────────────────────────────────

// runSkillsPublish handles `wuphf skills publish`.
//
// Stages:
//  1. Parse + validate args / flags.
//  2. Resolve the skill path on disk (slug → wiki path, or literal path).
//  3. Read + ParseSkillMarkdown to get a real frontmatter + body.
//  4. Build the manifest.
//  5. Either dry-run print, or shell out to `gh` to open the PR.
func runSkillsPublish(args []string) {
	fs := flag.NewFlagSet("skills publish", flag.ContinueOnError)
	to := fs.String("to", "", "Destination hub: anthropics, claude-marketplace, lobehub, or github:owner/repo")
	dryRun := fs.Bool("dry-run", false, "Print the manifest + would-be PR body, do not open the PR")
	message := fs.String("message", "", "Optional addition to the PR body (defaults to the skill description)")
	ghTokenFlag := fs.String("github-token", "", "Override gh's saved token; falls back to GH_TOKEN env when blank")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf skills publish — open a PR contributing your skill to a public hub")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf skills publish <slug>          --to anthropics")
		fmt.Fprintln(os.Stderr, "  wuphf skills publish team/skills/x.md --to lobehub")
		fmt.Fprintln(os.Stderr, "  wuphf skills publish <slug>          --to github:owner/repo")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	positional := fs.Args()
	if len(positional) == 0 {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "\nerror: provide a skill slug or markdown path")
		os.Exit(2)
	}
	target := positional[0]
	hub := strings.TrimSpace(*to)
	if hub == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "\nerror: --to is required")
		os.Exit(2)
	}
	if _, err := skillpublish.HubRepo(hub); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	// Resolve the on-disk path.
	skillPath, err := resolveSkillPath(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	content, err := os.ReadFile(skillPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read skill %s: %v\n", skillPath, err)
		os.Exit(1)
	}
	fm, body, err := team.ParseSkillMarkdown(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse %s: %v\n", skillPath, err)
		os.Exit(1)
	}
	manifest, err := skillpublish.BuildManifest(skillpublishFrontmatterFromTeam(fm), body, repoSlugForSource(), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: build manifest: %v\n", err)
		os.Exit(1)
	}

	hubFile, err := skillpublish.HubFilePath(hub, manifest.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	hubRepo, _ := skillpublish.HubRepo(hub)
	branch := skillpublish.PublishBranchName(manifest.Name, time.Now().UTC())
	prTitle := fmt.Sprintf("Publish %s skill", manifest.Name)
	prBody := buildPublishPRBody(manifest, *message)

	if *dryRun {
		fmt.Println("DRY RUN — would publish the following manifest + PR:")
		fmt.Println()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(manifest)
		fmt.Println()
		fmt.Printf("Target repo : %s\n", hubRepo)
		fmt.Printf("Target file : %s\n", hubFile)
		fmt.Printf("Branch      : %s\n", branch)
		fmt.Printf("PR title    : %s\n", prTitle)
		fmt.Println("PR body     :")
		fmt.Println(indentBlock(prBody, "  "))
		return
	}

	// Real publish — needs gh, gh auth, and a temp clone of the target repo.
	if err := ensureGHReady(*ghTokenFlag); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	prURL, err := publishViaGH(publishParams{
		ctx:        ctx,
		hub:        hub,
		hubRepo:    hubRepo,
		hubFile:    hubFile,
		branch:     branch,
		baseBranch: skillpublish.HubBaseBranch(hub),
		prTitle:    prTitle,
		prBody:     prBody,
		commitMsg:  fmt.Sprintf("Add %s skill", manifest.Name),
		fileBytes:  content, // publish the SKILL.md verbatim
		ghToken:    strings.TrimSpace(*ghTokenFlag),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: publish failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Published %s -> %s\n", manifest.Name, prURL)
}

type publishParams struct {
	ctx        context.Context
	hub        string
	hubRepo    string
	hubFile    string
	branch     string
	baseBranch string
	prTitle    string
	prBody     string
	commitMsg  string
	fileBytes  []byte
	ghToken    string
}

// publishViaGH performs the actual fork/clone/commit/PR dance via the gh CLI.
//
// Steps:
//  1. Fork the target hub repo into the user's account (idempotent — gh
//     returns success if the fork already exists).
//  2. Clone the fork into a temp dir.
//  3. Create a fresh branch from the base.
//  4. Write the SKILL.md to the right path.
//  5. Commit, push, and `gh pr create` against the upstream.
//
// All gh invocations are timeout-bounded and inherit GH_TOKEN if the caller
// passed --github-token.
func publishViaGH(p publishParams) (string, error) {
	tmpDir, err := os.MkdirTemp("", "wuphf-skill-publish-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	env := os.Environ()
	if p.ghToken != "" {
		env = append(env, "GH_TOKEN="+p.ghToken)
	}

	// 1. Fork (idempotent — re-running a publish reuses the existing fork).
	//    gh prints to stderr when the fork already exists; we only fail on a
	//    non-zero exit because that signals a real auth/network problem.
	if err := runGH(p.ctx, tmpDir, env, "repo", "fork", p.hubRepo, "--clone=false"); err != nil {
		return "", fmt.Errorf("fork %s: %w", p.hubRepo, err)
	}

	// 2. Resolve the authenticated user so we can clone the user's fork (not
	//    the upstream — gh repo clone has no `--repo` flag and does not
	//    auto-rewrite to the fork). Earlier versions of this code cloned
	//    p.hubRepo directly, which silently cloned the read-only upstream
	//    and then failed at `git push`.
	//
	//    A token without `read:user` scope returns either a 4xx error or a
	//    body without `.login`. Surface enough detail so the user can
	//    distinguish "token lacks scope" from "not logged in" — the
	//    previous error message steered all failures toward
	//    `gh auth login`, which is the wrong fix for token-based auth.
	authUser, err := captureGH(p.ctx, tmpDir, env, "api", "user", "--jq", ".login // .message // empty")
	if err != nil {
		return "", fmt.Errorf("resolve gh auth user via `gh api user`: %w (your token may lack `read:user` scope; for fine-grained tokens, ensure the user metadata permission is enabled)", err)
	}
	authUser = strings.TrimSpace(authUser)
	if authUser == "" {
		// `gh api user --jq` returned empty stdout. Re-fetch the raw body
		// for diagnostics so the caller can see whatever GitHub actually
		// said (token revoked, scope missing, installation token, etc.).
		raw, _ := captureGH(p.ctx, tmpDir, env, "api", "user")
		return "", fmt.Errorf("gh api user did not return a `.login` field — gh response was: %s (likely missing `read:user` scope or an installation/app token)", strings.TrimSpace(raw))
	}
	if strings.HasPrefix(authUser, "Bad credentials") || strings.HasPrefix(authUser, "Resource not accessible") || strings.Contains(authUser, "Not Found") {
		// `.message` came back instead of `.login` — surface verbatim.
		return "", fmt.Errorf("gh api user failed: %s", authUser)
	}
	_, hubName, ok := strings.Cut(p.hubRepo, "/")
	if !ok || hubName == "" {
		return "", fmt.Errorf("hubRepo %q must be owner/name", p.hubRepo)
	}
	forkRepo := authUser + "/" + hubName

	// GitHub's fork API returns before the fork is fully provisioned. Try
	// the clone up to 3× with a 2/4/8s backoff so a brand-new fork has
	// time to land — gh's own retry loop in `gh repo fork --clone` waits
	// up to ~30s, so 14s of backoff plus the gh request times themselves
	// is in the right ballpark for new orgs / first-time forks.
	cloneDir := filepath.Join(tmpDir, "fork")
	cloneBackoff := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	var cloneErr error
	for i := 0; i < 3; i++ {
		cloneErr = runGH(p.ctx, tmpDir, env, "repo", "clone", forkRepo, cloneDir, "--", "--depth=1", "--branch="+p.baseBranch)
		if cloneErr == nil {
			break
		}
		// Remove any partial clone before retrying so the next attempt
		// can start fresh.
		_ = os.RemoveAll(cloneDir)
		// Last attempt: don't sleep, just fall through.
		if i == 2 {
			break
		}
		select {
		case <-p.ctx.Done():
			return "", fmt.Errorf("clone fork cancelled: %w", p.ctx.Err())
		case <-time.After(cloneBackoff[i]):
		}
	}
	if cloneErr != nil {
		return "", fmt.Errorf("clone fork %s after 3 attempts (~14s of backoff): %w", forkRepo, cloneErr)
	}

	// 3. Branch from base.
	if err := runGit(p.ctx, cloneDir, env, "checkout", "-b", p.branch); err != nil {
		return "", fmt.Errorf("create branch: %w", err)
	}

	// 4. Write the SKILL.md to the hub-specific path.
	target := filepath.Join(cloneDir, p.hubFile)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, p.fileBytes, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}

	// 5. Commit + push + PR.
	if err := runGit(p.ctx, cloneDir, env, "add", p.hubFile); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	if err := runGit(p.ctx, cloneDir, env, "commit", "-m", p.commitMsg); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	if err := runGit(p.ctx, cloneDir, env, "push", "-u", "origin", p.branch); err != nil {
		return "", fmt.Errorf("git push: %w", err)
	}

	prURL, err := captureGH(p.ctx, cloneDir, env,
		"pr", "create",
		"--repo", p.hubRepo,
		"--base", p.baseBranch,
		"--head", p.branch,
		"--title", p.prTitle,
		"--body", p.prBody,
	)
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w", err)
	}
	return strings.TrimSpace(prURL), nil
}

// runGH wraps `gh` with a timeout, dir, env, and stderr passthrough so the
// user sees gh's own diagnostics inline. The parent ctx (from
// signal.NotifyContext) is honoured so a Ctrl-C during a long clone /
// push / pr-create cancels cleanly.
func runGH(parent context.Context, dir string, env []string, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, ghCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stderr // gh writes progress to stdout; surface as stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// captureGH is runGH but with stdout captured (used by `gh pr create` so we
// can echo back the PR URL). Honours the parent ctx for cancellation.
func captureGH(parent context.Context, dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, ghCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stderr = os.Stderr
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// runGit shells out to git. We rely on the user's git binary rather than
// importing a pure-Go implementation because it inherits the user's
// credential helper, gpg signing, etc. Honours the parent ctx for
// cancellation so push/checkout/commit can be interrupted.
func runGit(parent context.Context, dir string, env []string, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, ghCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureGHReady checks that gh is installed and authenticated before we
// start cloning. Without this check, the failure mode is a confusing git
// error several seconds in.
func ensureGHReady(tokenFlag string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found in PATH; install from https://cli.github.com")
	}
	// If the caller passed --github-token (or set GH_TOKEN), gh treats that
	// as authenticated. Skip the auth-status probe in that case so we don't
	// noisily fail when the user is intentionally using an ephemeral token.
	if strings.TrimSpace(tokenFlag) != "" || strings.TrimSpace(os.Getenv("GH_TOKEN")) != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("gh is installed but not authenticated; run `gh auth login` first")
	}
	return nil
}

// resolveSkillPath turns either a literal path or a slug into an on-disk path.
//
// Rules:
//   - if the input contains a "/" or ends in ".md", treat it as a literal path;
//   - otherwise treat it as a slug and resolve to <wikiRoot>/team/skills/{slug}.md.
func resolveSkillPath(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("skill name or path is required")
	}
	if strings.Contains(input, "/") || strings.HasSuffix(input, ".md") {
		// Literal path — accept absolute or relative.
		if _, err := os.Stat(input); err != nil {
			return "", fmt.Errorf("skill file not found at %s: %w", input, err)
		}
		abs, err := filepath.Abs(input)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path: %w", err)
		}
		return abs, nil
	}
	// Slug — resolve under the wiki root.
	root := team.WikiRootDir()
	candidate := filepath.Join(root, "team", "skills", input+".md")
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("skill %q not found at %s: %w", input, candidate, err)
	}
	return candidate, nil
}

// repoSlugForSource picks a stable provenance slug for the manifest's
// `source` field. We use the basename of the workspace dir (cwd) so multiple
// teams publishing from different repos still produce distinct sources.
// Falls back to "wuphf" when cwd is unavailable.
func repoSlugForSource() string {
	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		return "wuphf"
	}
	return filepath.Base(cwd)
}

// skillpublishFrontmatterFromTeam shrinks team.SkillFrontmatter down to the
// minimal shape skillpublish needs. Keeping the conversion local avoids an
// import cycle if/when team starts depending on publish helpers.
func skillpublishFrontmatterFromTeam(fm team.SkillFrontmatter) skillpublish.FrontmatterLike {
	return skillpublish.FrontmatterLike{
		Name:        fm.Name,
		Description: fm.Description,
		Version:     fm.Version,
		License:     fm.License,
	}
}

// buildPublishPRBody composes the PR body. Defaults to the skill's
// description; appends caller-provided extra prose under it.
//
// Provenance is captured by m.Source (sanitised to [a-z0-9-]). We do
// NOT include the local working directory — PRs land in a public hub
// repo and `Workspace: /Users/<name>/...` would leak the user's home
// directory layout to anyone reading the PR.
func buildPublishPRBody(m skillpublish.Manifest, extra string) string {
	var b strings.Builder
	b.WriteString(m.Description)
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Skill: `%s` (v%s, %s)\n", m.Name, m.Version, m.License))
	b.WriteString(fmt.Sprintf("Source: `%s` (published via WUPHF skills compiler)\n", m.Source))
	b.WriteString(fmt.Sprintf("Published at: %s\n", m.PublishedAt))
	if strings.TrimSpace(extra) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(extra))
		b.WriteString("\n")
	}
	return b.String()
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// ── install ────────────────────────────────────────────────────────────────

// runSkillsInstall handles `wuphf skills install`. Reverse of publish.
func runSkillsInstall(args []string) {
	fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
	from := fs.String("from", "", "Source hub: anthropics, claude-marketplace, lobehub, or github:owner/repo")
	channel := fs.String("channel", "general", "Channel to log the proposal into")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf skills install — pull a public skill into your team's wiki as a proposal")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf skills install <name> --from anthropics")
		fmt.Fprintln(os.Stderr, "  wuphf skills install <name> --from github:owner/repo")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	positional := fs.Args()
	if len(positional) == 0 {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "\nerror: provide a skill name to install")
		os.Exit(2)
	}
	name := strings.TrimSpace(positional[0])
	hub := strings.TrimSpace(*from)
	if hub == "" {
		fs.Usage()
		fmt.Fprintln(os.Stderr, "\nerror: --from is required")
		os.Exit(2)
	}

	rawURL, err := skillpublish.HubURL(hub, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	// One ctx for the whole install so Ctrl-C cancels both the GitHub
	// raw fetch AND the broker POST. Earlier this was set up only for
	// the broker post, so a hung github.com fetch had to wait for the
	// HTTP timeout.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	body, err := fetchURL(ctx, rawURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch %s: %v\n", rawURL, err)
		os.Exit(1)
	}
	fm, content, err := team.ParseSkillMarkdown(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse fetched skill: %v\n", err)
		os.Exit(1)
	}

	// Validate the fetched name before it touches the broker payload — a
	// hub repo could publish a SKILL.md with a path-traversal-shaped name
	// (e.g. "../../etc/x") and we must reject it before created_by /
	// content reach the broker.
	if _, err := skillpublish.BuildManifest(skillpublishFrontmatterFromTeam(fm), content, "", time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "error: fetched skill failed validation: %v\n", err)
		os.Exit(1)
	}

	// Post to the broker's /skills endpoint as `create`. The user invoking
	// `wuphf skills install` IS the human approval — we don't need the
	// proposal queue here. (Earlier this code used action=propose with
	// `created_by="hub:..."`, which always 403'd because the broker
	// requires created_by to resolve to a registered member for proposals.)
	createdBy := "hub:" + sanitizeHubLabel(hub)
	payload := map[string]any{
		"action":      "create",
		"name":        fm.Name,
		"title":       fm.Name,
		"description": fm.Description,
		"content":     content,
		"created_by":  createdBy,
		"channel":     strings.TrimSpace(*channel),
	}
	if err := postBrokerSkill(ctx, payload); err != nil {
		fmt.Fprintf(os.Stderr, "error: post to broker: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Installed %s from %s (status=active). Edit at /skills.\n", fm.Name, hub)
}

// fetchURL grabs a public raw URL with a bounded timeout and returns the
// response body. 4 MiB cap is plenty for a SKILL.md and refuses gigantic
// payloads accidentally hosted at the path.
//
// Redirects are restricted to GitHub's raw-content host. Without this
// guard a malicious `github:` hub could 302 the install fetch to an
// attacker-controlled host, and the post-install BuildManifest +
// broker POST would then accept content from anywhere.
func fetchURL(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, skillsHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: skillsHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Case-fold the host so "RAW.githubusercontent.com" or any
			// other case variant from a misconfigured hub redirector
			// still passes (Go preserves URL host case for non-IDN
			// hosts). We deliberately do NOT accept *.githubusercontent.com
			// subdomain shards — if GitHub ever serves raw content from a
			// new shard, prefer reviewing the change here over silently
			// trusting any subdomain.
			if !strings.EqualFold(req.URL.Hostname(), "raw.githubusercontent.com") {
				return fmt.Errorf("refusing redirect to non-raw-github host %q (a hub redirected the install fetch off-platform)", req.URL.Host)
			}
			return nil
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", res.StatusCode, rawURL)
	}
	return io.ReadAll(io.LimitReader(res.Body, 4*1024*1024))
}

// postBrokerSkill posts to the local broker's /skills endpoint, mirroring the
// auth + base URL conventions used elsewhere in the CLI.
func postBrokerSkill(ctx context.Context, payload map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, skillsHTTPTimeout)
	defer cancel()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	url := brokerURL("/skills")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := currentBrokerAuthToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("broker unreachable at %s: %w (start it with `wuphf` first)", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker POST /skills failed: %s %s", res.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// reorderFlagsFirst is a small ergonomic helper: Go's stdlib flag package
// stops parsing at the first non-flag token, so `wuphf skills publish my-slug
// --to anthropics` would otherwise treat `--to` as another positional. This
// helper walks args, detects flag tokens (and their value-arg when not in
// `--key=value` form), and emits them ahead of positionals.
//
// The reorder is deliberately conservative — anything past `--` stays in
// place, and unknown flag-looking tokens are still treated as flags so the
// downstream Parse can produce a friendly error message.
func reorderFlagsFirst(args []string) []string {
	flags := []string{}
	positional := []string{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			flags = append(flags, args[i:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			// If the flag is in `--key=value` form or is a `-h` style
			// boolean, no value-arg follows. Otherwise, eagerly consume
			// the next token. We can't introspect the FlagSet from here,
			// so we apply the standard heuristic: if the next token does
			// not itself start with `-`, treat it as the value.
			if !strings.Contains(a, "=") && i+1 < len(args) {
				next := args[i+1]
				if !strings.HasPrefix(next, "-") {
					flags = append(flags, next)
					i++
				}
			}
			i++
			continue
		}
		positional = append(positional, a)
		i++
	}
	return append(flags, positional...)
}

// sanitizeHubLabel returns a hub label suitable for `created_by="hub:<label>"`.
// `github:owner/repo` collapses to `github-owner-repo` so the broker sees a
// stable identifier per source.
func sanitizeHubLabel(hub string) string {
	hub = strings.TrimSpace(hub)
	hub = strings.ReplaceAll(hub, ":", "-")
	hub = strings.ReplaceAll(hub, "/", "-")
	return strings.ToLower(hub)
}
