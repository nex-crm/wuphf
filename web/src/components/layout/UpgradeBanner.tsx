import { useCallback, useEffect, useMemo, useState } from 'react'
import { getVersion } from '../../api/client'

const REPO = 'nex-crm/wuphf'
const NPM_PACKAGE = 'wuphf'
const UPGRADE_COMMAND = 'npm install -g wuphf@latest'
const DISMISSED_KEY = 'wuphf-upgrade-dismissed-version'

interface CommitEntry {
  type: string
  scope: string
  description: string
  pr: string | null
  sha: string
  breaking: boolean
}

interface ChangelogState {
  loading: boolean
  error: string | null
  commits: CommitEntry[]
}

interface GitHubCompareCommit {
  sha: string
  commit: { message: string }
}

interface GitHubCompareResponse {
  commits?: GitHubCompareCommit[]
}

const TYPE_LABELS: Array<{ type: string; label: string }> = [
  { type: 'breaking', label: 'Breaking changes' },
  { type: 'feat', label: 'New features' },
  { type: 'fix', label: 'Bug fixes' },
  { type: 'perf', label: 'Performance' },
  { type: 'refactor', label: 'Refactoring' },
  { type: 'docs', label: 'Documentation' },
  { type: 'other', label: 'Other changes' },
]

const KNOWN_TYPES = new Set(TYPE_LABELS.map((t) => t.type))

// Accept v0.79, 0.79.15, 0.79.15.1, 1.2.3-rc.4. Anything else is rejected so
// the URL override cannot inject arbitrary path segments into the GitHub
// compare API call.
const VERSION_RE = /^v?\d+(\.\d+){1,3}(-[\w.]+)?$/

function stripV(v: string): string {
  return v.replace(/^v/, '')
}

function compareVersions(a: string, b: string): number {
  const pa = stripV(a).split('.').map((n) => parseInt(n, 10) || 0)
  const pb = stripV(b).split('.').map((n) => parseInt(n, 10) || 0)
  const len = Math.max(pa.length, pb.length)
  for (let i = 0; i < len; i++) {
    const x = pa[i] ?? 0
    const y = pb[i] ?? 0
    if (x !== y) return x < y ? -1 : 1
  }
  return 0
}

// Matches only a trailing `(#N)` reference so an inline `(#42)` inside the
// description is preserved as text (matches the Go parser).
const TRAILING_PR_RE = /\s*\(#(\d+)\)\s*$/

function extractPR(s: string): string | null {
  const m = s.match(TRAILING_PR_RE)
  return m ? m[1] : null
}

function parseCommit(message: string, sha: string): CommitEntry {
  const subject = (message.split('\n')[0] ?? '').trim()
  const m = subject.match(
    /^(feat|fix|perf|refactor|docs|chore|test|build|ci|style|revert)(\([^)]+\))?(!)?:\s*(.+?)\s*$/i,
  )
  if (!m) {
    return {
      type: 'other',
      scope: '',
      description: subject,
      pr: extractPR(subject),
      sha,
      breaking: false,
    }
  }
  const type = m[1].toLowerCase()
  const scope = (m[2] ?? '').replace(/[()]/g, '')
  const breaking = m[3] === '!'
  const rest = m[4]
  return {
    type,
    scope,
    description: rest.replace(/\s*\(#\d+\)\s*$/, '').trim(),
    pr: extractPR(rest),
    sha,
    breaking,
  }
}

function groupCommits(commits: CommitEntry[]) {
  const buckets = new Map<string, CommitEntry[]>()
  for (const c of commits) {
    const key = c.breaking ? 'breaking' : KNOWN_TYPES.has(c.type) ? c.type : 'other'
    const list = buckets.get(key) ?? []
    list.push(c)
    buckets.set(key, list)
  }
  return TYPE_LABELS.flatMap(({ type, label }) => {
    const entries = buckets.get(type)
    if (!entries || entries.length === 0) return []
    return [{ label, entries }]
  })
}

// Override pair from the URL — `?upgrade-from=v0.79.10&upgrade-to=v0.79.15` —
// lets QA/screenshots preview the banner without a real version mismatch.
// Both values are validated against VERSION_RE so the override cannot inject
// arbitrary path segments into the GitHub compare API call we make later.
function readForcedPair(): { from: string; to: string } | null {
  if (typeof window === 'undefined') return null
  const p = new URLSearchParams(window.location.search)
  const from = p.get('upgrade-from')
  const to = p.get('upgrade-to')
  if (!from || !to) return null
  if (!VERSION_RE.test(from) || !VERSION_RE.test(to)) return null
  return { from, to }
}

export function UpgradeBanner() {
  const forced = useMemo(readForcedPair, [])
  // Suppress in dev so local devs aren't nagged by the placeholder VERSION.
  // The URL override bypasses the dev guard.
  const enabled = forced != null || !import.meta.env.DEV

  const [current, setCurrent] = useState<string | null>(forced?.from ?? null)
  const [latest, setLatest] = useState<string | null>(forced?.to ?? null)
  const [dismissed, setDismissed] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [copied, setCopied] = useState(false)
  const [changelog, setChangelog] = useState<ChangelogState>({
    loading: false,
    error: null,
    commits: [],
  })

  useEffect(() => {
    if (!enabled || forced) return
    let cancelled = false
    void Promise.all([
      getVersion().catch(() => null),
      fetch(`https://registry.npmjs.org/${NPM_PACKAGE}/latest`)
        .then((r) => (r.ok ? (r.json() as Promise<{ version?: string }>) : null))
        .catch(() => null),
    ]).then(([cur, npm]) => {
      if (cancelled) return
      if (cur?.version) setCurrent(cur.version)
      if (npm?.version) setLatest(String(npm.version))
    })
    return () => {
      cancelled = true
    }
  }, [enabled, forced])

  useEffect(() => {
    if (!latest) return
    const d = localStorage.getItem(DISMISSED_KEY)
    setDismissed(d === latest)
  }, [latest])

  const upgradeNeeded = useMemo(() => {
    if (!current || !latest) return false
    return compareVersions(current, latest) < 0
  }, [current, latest])

  const compareUrl = useMemo(() => {
    if (!current || !latest) return ''
    return `https://github.com/${REPO}/compare/v${stripV(current)}...v${stripV(latest)}`
  }, [current, latest])

  const fetchChangelog = useCallback(async () => {
    if (!current || !latest) return
    setChangelog({ loading: true, error: null, commits: [] })
    try {
      const url = `https://api.github.com/repos/${REPO}/compare/v${stripV(current)}...v${stripV(latest)}`
      const r = await fetch(url, { headers: { Accept: 'application/vnd.github+json' } })
      if (!r.ok) throw new Error(`GitHub ${r.status}`)
      const data = (await r.json()) as GitHubCompareResponse
      const commits = (data.commits ?? []).map((c) =>
        parseCommit(c.commit?.message ?? '', c.sha ?? ''),
      )
      setChangelog({ loading: false, error: null, commits })
    } catch (e) {
      setChangelog({
        loading: false,
        error: e instanceof Error ? e.message : String(e),
        commits: [],
      })
    }
  }, [current, latest])

  const toggleExpanded = useCallback(() => {
    setExpanded((prev) => {
      const next = !prev
      if (next && changelog.commits.length === 0 && !changelog.loading && !changelog.error) {
        void fetchChangelog()
      }
      return next
    })
  }, [changelog.commits.length, changelog.loading, changelog.error, fetchChangelog])

  const copyUpgradeCommand = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(UPGRADE_COMMAND)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard API unavailable; ignore.
    }
  }, [])

  const dismiss = useCallback(() => {
    if (latest) localStorage.setItem(DISMISSED_KEY, latest)
    setDismissed(true)
  }, [latest])

  if (!enabled || !upgradeNeeded || dismissed) return null
  if (!current || !latest) return null

  const grouped = groupCommits(changelog.commits)

  return (
    <div className="upgrade-banner" role="status">
      <div className="upgrade-banner-row">
        <div className="upgrade-banner-content">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="17 8 12 3 7 8" />
            <line x1="12" y1="3" x2="12" y2="15" />
          </svg>
          <span>
            Update available: <strong>v{stripV(current)}</strong> →{' '}
            <strong>v{stripV(latest)}</strong>
          </span>
          <button
            type="button"
            className="upgrade-banner-link"
            onClick={toggleExpanded}
            aria-expanded={expanded}
          >
            {expanded ? 'Hide changes' : "What's new"}
          </button>
          <a
            className="upgrade-banner-link"
            href={compareUrl}
            target="_blank"
            rel="noopener noreferrer"
          >
            View on GitHub
          </a>
        </div>
        <div className="upgrade-banner-actions">
          <button
            type="button"
            className="upgrade-banner-copy"
            onClick={copyUpgradeCommand}
            title="Click to copy"
          >
            <code>{UPGRADE_COMMAND}</code>
            <span className="upgrade-banner-copy-hint">{copied ? 'Copied!' : 'Copy'}</span>
          </button>
          <button
            type="button"
            className="upgrade-banner-dismiss"
            onClick={dismiss}
            aria-label="Dismiss"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>
      </div>
      {expanded && (
        <div className="upgrade-banner-changelog">
          {changelog.loading && (
            <div className="upgrade-banner-changelog-status">Loading changes…</div>
          )}
          {changelog.error && (
            <div className="upgrade-banner-changelog-status">
              Could not load changelog ({changelog.error}).{' '}
              <a href={compareUrl} target="_blank" rel="noopener noreferrer">
                View on GitHub
              </a>
              .
            </div>
          )}
          {!changelog.loading && !changelog.error && changelog.commits.length === 0 && (
            <div className="upgrade-banner-changelog-status">No commits found.</div>
          )}
          {grouped.map((group) => (
            <div key={group.label} className="upgrade-banner-changelog-group">
              <div className="upgrade-banner-changelog-label">{group.label}</div>
              <ul className="upgrade-banner-changelog-list">
                {group.entries.map((entry) => (
                  <li key={entry.sha}>
                    {entry.scope && (
                      <span className="upgrade-banner-scope">{entry.scope}</span>
                    )}{' '}
                    {entry.description}
                    {entry.pr && (
                      <>
                        {' '}
                        <a
                          href={`https://github.com/${REPO}/pull/${entry.pr}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="upgrade-banner-pr"
                        >
                          #{entry.pr}
                        </a>
                      </>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
