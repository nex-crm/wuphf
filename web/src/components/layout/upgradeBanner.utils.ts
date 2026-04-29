// Pure helpers for the UpgradeBanner. Split out so they can be unit-tested
// without React, and so the parser logic stays close to its Go twin in
// internal/upgradecheck for easy side-by-side review.

export interface CommitEntry {
  type: string;
  scope: string;
  description: string;
  pr: string | null;
  sha: string;
  breaking: boolean;
}

export const TYPE_LABELS: Array<{ type: string; label: string }> = [
  { type: "breaking", label: "Breaking changes" },
  { type: "feat", label: "New features" },
  { type: "fix", label: "Bug fixes" },
  { type: "perf", label: "Performance" },
  { type: "refactor", label: "Refactoring" },
  { type: "docs", label: "Documentation" },
  { type: "other", label: "Other changes" },
];

const KNOWN_TYPES = new Set(TYPE_LABELS.map((t) => t.type));

// Accept v0.79, 0.79.15, 0.79.15.1, 1.2.3-rc.4, 1.2.3-beta-1, 1.2.3+build.5.
// Character class mirrors internal/upgradecheck/upgradecheck.go's
// VersionParamRE and internal/team/broker.go's upgradeVersionParam — keep
// all three in sync. Hyphen is allowed inside the suffix character class
// so `-beta-1` validates.
export const VERSION_RE =
  /^v?\d+(\.\d+){1,3}(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$/;

// Buildinfo's "dev" sentinel — keep in sync with the Go twins:
// buildinfo.IsDev (canonical literal sentinel) and upgradecheck.IsDevVersion
// (extended check that also rejects garbage and sub-0.1.0 versions). This
// helper applies the full extended logic so the URL-override preview path
// classifies stale-VERSION builds as dev too.
//
// Note: on the production banner path, UpgradeBanner.tsx trusts the
// server-authoritative `is_dev_build` flag from /upgrade-check first, so
// this local check is dead code in the #350 reproducer. It only fires on
// the URL-override (?upgrade-from=…&upgrade-to=…) preview path used by QA
// and screenshots — keeping the twin in sync with the Go side prevents a
// future preview pair from rendering a downgrade-shaped banner during
// pre-launch sweeps.
export function isDevVersion(v: string | null | undefined): boolean {
  if (!v) return true;
  const t = v.trim();
  if (t === "" || t === "dev") return true;
  if (!VERSION_RE.test(t)) return true;
  if (compareVersions(t, "0.1.0") < 0) return true;
  return false;
}

// Trim FIRST then strip leading `v` so " v0.79.10" parses correctly. Mirror
// of the Go behaviour: strings.TrimPrefix(strings.TrimSpace(v), "v").
export function stripV(v: string): string {
  return v.trim().replace(/^v/, "");
}

// Compare dotted-numeric versions. Pre-release (`-rc.1`) AND build-metadata
// (`+build.5`) suffixes are stripped before comparison so all of
// `0.79.10`, `0.79.10-rc.1`, `0.79.10+build.5` sort equal — matches the Go
// `compareVersions` behaviour in internal/upgradecheck.
export function compareVersions(a: string, b: string): number {
  const pa = splitVersion(a);
  const pb = splitVersion(b);
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i++) {
    const x = pa[i] ?? 0;
    const y = pb[i] ?? 0;
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}

function splitVersion(v: string): number[] {
  let s = stripV(v);
  // Strip pre-release suffix first, then build metadata (or vice
  // versa — order doesn't matter, both are dropped before splitting).
  const dash = s.indexOf("-");
  if (dash >= 0) s = s.slice(0, dash);
  const plus = s.indexOf("+");
  if (plus >= 0) s = s.slice(0, plus);
  return s.split(".").map((n) => Number.parseInt(n, 10) || 0);
}

// Trailing PR ref: only strip a `(#N)` at end-of-string, leaving inline
// references like "handle (#42) properly" intact. Matches the Go regex.
const TRAILING_PR_RE = /\s*\(#(\d+)\)\s*$/;

export function extractPR(s: string): string | null {
  const m = s.match(TRAILING_PR_RE);
  return m ? m[1] : null;
}

// Build a GitHub /pull/<n> URL only when the PR token is a clean number.
// Returns null for anything else so a future broker change emitting a
// non-numeric ref (or a parser bug) can't produce a malformed URL the
// banner would still render as a clickable link.
export function prGitHubURL(repo: string, pr: string): string | null {
  return /^\d+$/.test(pr) ? `https://github.com/${repo}/pull/${pr}` : null;
}

// Conventional-commit parser. Mirrors internal/upgradecheck.parseCommit so
// the CLI and web banner render the same text for the same input. Capture
// groups: 1=type, 2=(scope), 3=! (breaking), 4=description.
const CONVENTIONAL_RE =
  /^(feat|fix|perf|refactor|docs|chore|test|build|ci|style|revert)(\([^)]+\))?(!)?:\s*(.+?)\s*$/i;

export function parseCommit(message: string, sha: string): CommitEntry {
  const subject = (message.split("\n")[0] ?? "").trim();
  const m = subject.match(CONVENTIONAL_RE);
  if (!m) {
    return {
      type: "other",
      scope: "",
      description: subject,
      pr: extractPR(subject),
      sha,
      breaking: false,
    };
  }
  const type = m[1].toLowerCase();
  const scope = (m[2] ?? "").replace(/[()]/g, "");
  const breaking = m[3] === "!";
  const rest = m[4];
  return {
    type,
    scope,
    description: rest.replace(TRAILING_PR_RE, "").trim(),
    pr: extractPR(rest),
    sha,
    breaking,
  };
}

// hasNotable mirrors internal/upgradecheck.Notable. Returns true iff at
// least one commit is a feat / fix / perf or carries the breaking marker.
// The banner uses this as the show/hide gate so a release that's purely
// docs/chore/refactor/test/ci/style/build doesn't trigger a banner.
//
// Failure-open intent: callers MUST default to `true` when they couldn't
// fetch the changelog at all — better to nag than to swallow a critical
// update. This function is for the "we have data, what does it say" case
// only; absence-of-data is a separate decision.
export function hasNotable(commits: CommitEntry[]): boolean {
  for (const c of commits) {
    if (c.breaking) return true;
    if (c.type === "feat" || c.type === "fix" || c.type === "perf") return true;
  }
  return false;
}

// isMajorBump compares the first dotted-numeric segment of from/to. Returns
// true when `to`'s major is strictly greater. Matches
// internal/upgradecheck.IsMajorBump. Used by the banner to force-show across
// a major-version line — bypasses dismiss entirely.
//
// Note: today auto-release.yml only knows feat-vs-not (no major bump on
// `feat!:` markers), so a major bump is a deliberate human action — exactly
// the kind of release we want to push. When auto-release learns to bump on
// breaking markers, this rule still does the right thing.
export function isMajorBump(from: string, to: string): boolean {
  const pf = splitVersion(from);
  const pt = splitVersion(to);
  return (pt[0] ?? 0) > (pf[0] ?? 0);
}

// decideShow centralises the banner's render gate so the matrix is
// unit-testable in isolation. Returns true iff the banner should mount.
//
// Rules, in order:
//   1. enabled=false → never show.
//   2. runPhase==="running" → always show. The user has initiated an
//      install; killing the banner mid-install would lose their progress
//      surface.
//   3. runPhase==="done" → defer to the same matrix as idle. The post-run
//      outcome row is part of the banner; if the user dismisses after
//      a failed install, honour that — don't keep the banner sticky just
//      because a run completed.
//   4. !upgradeNeeded → hide. Nothing to upgrade.
//   5. forceMajor → show. Major bumps bypass dismiss because they're
//      deliberate human actions in our auto-release setup (see comment
//      block in UpgradeBanner.tsx).
//   6. silenced → hide. User explicitly muted up to or past `latest`.
//   7. !notableGate → hide. The diff has zero feat/fix/perf/breaking.
//   8. Otherwise → show.
export function decideShow(input: {
  enabled: boolean;
  runPhase: "idle" | "running" | "done";
  upgradeNeeded: boolean;
  forceMajor: boolean;
  silenced: boolean;
  notableGate: boolean;
}): boolean {
  if (!input.enabled) return false;
  if (input.runPhase === "running") return true;
  if (!input.upgradeNeeded) return false;
  if (input.forceMajor) return true;
  if (input.silenced) return false;
  return input.notableGate;
}

export function groupCommits(commits: CommitEntry[]) {
  const buckets = new Map<string, CommitEntry[]>();
  for (const c of commits) {
    const key = c.breaking
      ? "breaking"
      : KNOWN_TYPES.has(c.type)
        ? c.type
        : "other";
    const list = buckets.get(key) ?? [];
    list.push(c);
    buckets.set(key, list);
  }
  return TYPE_LABELS.flatMap(({ type, label }) => {
    const entries = buckets.get(type);
    if (!entries || entries.length === 0) return [];
    return [{ label, entries }];
  });
}

// Override pair from the URL — `?upgrade-from=v0.79.10&upgrade-to=v0.79.15` —
// lets QA/screenshots preview the banner without a real version mismatch.
// Both values are validated against VERSION_RE so the override cannot inject
// arbitrary path segments into any downstream API call.
export function readForcedPair(): { from: string; to: string } | null {
  if (typeof window === "undefined") return null;
  const p = new URLSearchParams(window.location.search);
  const from = p.get("upgrade-from");
  const to = p.get("upgrade-to");
  if (!(from && to)) return null;
  if (!(VERSION_RE.test(from) && VERSION_RE.test(to))) return null;
  return { from, to };
}

// Best-effort localStorage wrappers. Safari private mode and sandboxed
// iframes throw synchronously on access, so an unguarded call would break
// the banner's click handler entirely (the user clicks X and nothing
// happens). Errors silently degrade to "no persistence".
export function safeLocalStorageGet(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function safeLocalStorageSet(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // ignore
  }
}
