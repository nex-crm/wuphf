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

// Buildinfo's "dev" sentinel — see internal/buildinfo/buildinfo.go. Keep
// in sync with upgradecheck.IsDevVersion. Also rejects garbage strings and
// any sub-0.1.0 version as a sentinel (#350: a stale VERSION file shipping
// "0.0.7.1" passed through this guard and produced a false "upgrade
// available v0.0.7.1 → v0.79.2" — actually a downgrade).
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
