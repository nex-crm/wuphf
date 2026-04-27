import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { get } from "../../api/client";
import {
  type CommitEntry,
  compareVersions,
  groupCommits,
  isDevVersion,
  parseCommit,
  readForcedPair,
  safeLocalStorageGet,
  safeLocalStorageSet,
  stripV,
  VERSION_RE,
} from "./upgradeBanner.utils";

const REPO = "nex-crm/wuphf";
const UPGRADE_COMMAND = "npm install -g wuphf@latest";
const DISMISSED_KEY = "wuphf-upgrade-dismissed-version";

interface UpgradeCheckResponse {
  current: string;
  latest: string;
  upgrade_available: boolean;
  is_dev_build: boolean;
  compare_url?: string;
  upgrade_command: string;
  error?: string;
}

interface UpgradeChangelogResponse {
  commits?: Array<{
    type: string;
    scope: string;
    description: string;
    pr: string;
    sha: string;
    breaking: boolean;
  }>;
  error?: string;
}

interface ChangelogState {
  loading: boolean;
  error: string | null;
  commits: CommitEntry[];
}

export function UpgradeBanner() {
  const forced = useMemo(readForcedPair, []);
  // Suppress in dev so local devs aren't nagged by the placeholder VERSION.
  // The URL override bypasses the dev guard.
  const enabled = forced !== null || !import.meta.env.DEV;

  const [current, setCurrent] = useState<string | null>(forced?.from ?? null);
  const [latest, setLatest] = useState<string | null>(forced?.to ?? null);
  const [dismissed, setDismissed] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const [changelog, setChangelog] = useState<ChangelogState>({
    loading: false,
    error: null,
    commits: [],
  });
  // Per-component latch so a successful (or non-abort-failed) fetch is
  // not retried when the user toggles expanded off and on again. Set in
  // the resolution callbacks (NOT at fetch-start) so a collapse-while-
  // loading leaves the ref unset and the next expand can retry.
  const changelogFetchedRef = useRef(false);

  useEffect(() => {
    if (!enabled || forced) return;
    // The AbortController here only flag-guards against post-unmount
    // setState — `get()` in api/client.ts doesn't currently accept an
    // AbortSignal, so the underlying fetch still completes server-side
    // (broker still does the upstream call). Threading signal through
    // the shared client is a follow-up that touches every caller of
    // get() and is out of scope for this PR.
    const ctl = new AbortController();
    void get<UpgradeCheckResponse>("/upgrade-check")
      .then((res) => {
        if (ctl.signal.aborted) return;
        if (res.current) setCurrent(res.current);
        if (res.latest) setLatest(res.latest);
      })
      .catch(() => {
        // Broker unreachable or returned a non-2xx — degrade silently.
      });
    return () => {
      ctl.abort();
    };
  }, [enabled, forced]);

  useEffect(() => {
    if (!latest) return;
    const d = safeLocalStorageGet(DISMISSED_KEY);
    setDismissed(d === latest);
  }, [latest]);

  // Drive the changelog fetch from `expanded`. Same caveat as the
  // upgrade-check effect above: the AbortController only flag-guards
  // setState on unmount; the broker still completes the GitHub call.
  // The "have we fetched" bit is latched in the resolution callbacks
  // (NOT at fetch start), so a collapse-while-loading leaves the ref
  // unset and the next expand can retry. The cleanup also resets the
  // loading state so a re-expand doesn't render a stale "Loading
  // changes…" caption.
  useEffect(() => {
    if (!expanded) return;
    if (!(current && latest)) return;
    if (changelogFetchedRef.current) return;
    const ctl = new AbortController();
    setChangelog({ loading: true, error: null, commits: [] });
    void get<UpgradeChangelogResponse>("/upgrade-changelog", {
      from: current,
      to: latest,
    })
      .then((data) => {
        if (ctl.signal.aborted) return;
        changelogFetchedRef.current = true;
        if (data.error) {
          setChangelog({ loading: false, error: data.error, commits: [] });
          return;
        }
        // The broker forwards entries already parsed by upgradecheck on
        // the Go side (with explicit JSON tags ensuring lowercase keys),
        // so the `c.type` branch is always taken in practice. The
        // parseCommit fallback is a forward-compat seam for a future
        // broker version that might emit raw commit messages — keep it
        // even though it's effectively dead code today.
        const commits: CommitEntry[] = (data.commits ?? []).map((c) =>
          c.type
            ? {
                type: c.type,
                scope: c.scope ?? "",
                description: c.description ?? "",
                pr: c.pr || null,
                sha: c.sha ?? "",
                breaking: !!c.breaking,
              }
            : parseCommit(c.description ?? "", c.sha ?? ""),
        );
        setChangelog({ loading: false, error: null, commits });
      })
      .catch((e: unknown) => {
        if (ctl.signal.aborted) return;
        // Latch on error too so the user sees the error message instead
        // of the next expand silently re-firing the same failing call.
        changelogFetchedRef.current = true;
        setChangelog({
          loading: false,
          error: e instanceof Error ? e.message : String(e),
          commits: [],
        });
      });
    return () => {
      ctl.abort();
      // Cleanup-while-loading means neither .then nor .catch will run.
      // Drop the loading caption so a re-expand doesn't show a stale
      // "Loading changes…" while the new fetch is being kicked off.
      setChangelog((prev) =>
        prev.loading ? { loading: false, error: null, commits: [] } : prev,
      );
    };
  }, [expanded, current, latest]);

  const upgradeNeeded = useMemo(() => {
    if (!(current && latest)) return false;
    if (isDevVersion(current)) return false;
    if (!(VERSION_RE.test(current) && VERSION_RE.test(latest))) return false;
    return compareVersions(current, latest) < 0;
  }, [current, latest]);

  const compareUrl = useMemo(() => {
    if (!(current && latest)) return "";
    return `https://github.com/${REPO}/compare/v${stripV(current)}...v${stripV(latest)}`;
  }, [current, latest]);

  const toggleExpanded = useCallback(() => {
    setExpanded((prev) => !prev);
  }, []);

  // Track the "Copied!" reset timer so an unmount within 1.5s of a copy
  // doesn't fire setCopied on a dead component (React swallows it but
  // the timer still owns a closure on the unmounted instance).
  const copyTimerRef = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (copyTimerRef.current !== null) {
        window.clearTimeout(copyTimerRef.current);
      }
    },
    [],
  );

  const copyUpgradeCommand = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(UPGRADE_COMMAND);
      setCopied(true);
      if (copyTimerRef.current !== null) {
        window.clearTimeout(copyTimerRef.current);
      }
      copyTimerRef.current = window.setTimeout(() => {
        copyTimerRef.current = null;
        setCopied(false);
      }, 1500);
    } catch {
      // Clipboard API unavailable; ignore.
    }
  }, []);

  const dismiss = useCallback(() => {
    if (latest) safeLocalStorageSet(DISMISSED_KEY, latest);
    setDismissed(true);
  }, [latest]);

  // Memoise the grouped commits so a render that doesn't change the
  // commit list (e.g. expand/collapse toggling) doesn't re-bucket. The
  // null-guard `if (!(current && latest)) return null;` previously sat
  // here as well — removed because upgradeNeeded already requires both.
  const grouped = useMemo(
    () => groupCommits(changelog.commits),
    [changelog.commits],
  );

  if (!(enabled && upgradeNeeded) || dismissed) return null;

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
            Update available: <strong>v{stripV(current)}</strong> →{" "}
            <strong>v{stripV(latest)}</strong>
          </span>
          <button
            type="button"
            className="upgrade-banner-link"
            onClick={toggleExpanded}
            aria-expanded={expanded}
          >
            {expanded ? "Hide changes" : "What's new"}
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
            <span className="upgrade-banner-copy-hint">
              {copied ? "Copied!" : "Copy"}
            </span>
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
              aria-hidden="true"
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
            <div className="upgrade-banner-changelog-status">
              Loading changes…
            </div>
          )}
          {changelog.error && (
            <div className="upgrade-banner-changelog-status">
              Could not load changelog ({changelog.error}).{" "}
              <a href={compareUrl} target="_blank" rel="noopener noreferrer">
                View on GitHub
              </a>
              .
            </div>
          )}
          {!(changelog.loading || changelog.error) &&
            changelog.commits.length === 0 && (
              <div className="upgrade-banner-changelog-status">
                No commits found.
              </div>
            )}
          {grouped.map((group) => (
            <div key={group.label} className="upgrade-banner-changelog-group">
              <div className="upgrade-banner-changelog-label">
                {group.label}
              </div>
              <ul className="upgrade-banner-changelog-list">
                {group.entries.map((entry) => (
                  <li key={entry.sha}>
                    {entry.scope ? (
                      <>
                        <span className="upgrade-banner-scope">
                          {entry.scope}
                        </span>{" "}
                      </>
                    ) : null}
                    {entry.description}
                    {entry.pr ? (
                      <>
                        {" "}
                        <a
                          href={`https://github.com/${REPO}/pull/${entry.pr}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="upgrade-banner-pr"
                        >
                          #{entry.pr}
                        </a>
                      </>
                    ) : null}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
