import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";

import { get, runUpgrade, type UpgradeRunResult } from "../../api/client";
import {
  type CommitEntry,
  compareVersions,
  decideShow,
  groupCommits,
  hasNotable,
  isDevVersion,
  isMajorBump,
  parseCommit,
  prGitHubURL,
  readForcedPair,
  safeLocalStorageGet,
  safeLocalStorageSet,
  stripV,
  VERSION_RE,
} from "./upgradeBanner.utils";

const REPO = "nex-crm/wuphf";
// SILENT_UP_TO_KEY stores the high-water-mark of "latest version the user
// has actively chosen to mute" — NOT just the dismissed version. This lets
// us re-show the banner the moment a notable commit lands AFTER the user
// last said "not now", instead of permanently muting until a new release
// happens to be the literal-equal version they last clicked-X on.
const SILENT_UP_TO_KEY = "wuphf-upgrade-silent-up-to";

interface UpgradeCheckResponse {
  current: string;
  latest: string;
  upgrade_available: boolean;
  is_dev_build: boolean;
  compare_url?: string;
  upgrade_command: string;
  // install_method/install_command are the server's view of what
  // POST /upgrade/run would ACTUALLY execute on this host (global vs
  // local install). The chip renders install_command verbatim so the
  // click target's text never lies. Older brokers omit these fields —
  // fall back to upgrade_command.
  install_method?: "global" | "local" | "unknown";
  install_command?: string;
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
  // ready=true means the fetch attempt has resolved — success, error, or
  // empty response. Distinct from `loading` because the show/hide gate
  // needs to know "have we tried yet" vs "is it in flight". Failure-open:
  // when ready=false, we treat the gate as undecided (don't hide).
  ready: boolean;
}

type RunState =
  | { phase: "idle" }
  | { phase: "running" }
  | { phase: "done"; result: UpgradeRunResult };

export function UpgradeBanner() {
  const forced = useMemo(readForcedPair, []);
  // Suppress in dev so local devs aren't nagged by the placeholder VERSION.
  // The URL override bypasses the dev guard.
  const enabled = forced !== null || !import.meta.env.DEV;

  const [current, setCurrent] = useState<string | null>(forced?.from ?? null);
  const [latest, setLatest] = useState<string | null>(forced?.to ?? null);
  // Server-authoritative dev-build flag (set from /upgrade-check). The Go
  // side has the canonical view of what counts as a dev build (buildinfo's
  // "" / "dev" sentinel) — trusting the server flag means a future
  // buildinfo change adding a new sentinel flows through automatically.
  // The URL-override path skips the server call so this stays false
  // (intentional: QA preview shouldn't be classified as dev).
  const [isDevBuildSrv, setIsDevBuildSrv] = useState(false);
  // installCommand: the literal command the broker would run on
  // /upgrade/run for this host. Falls back to the canonical
  // `npm install -g …` doc string when the server hasn't decided yet
  // OR sent a "unknown" install method. The chip uses this for its
  // label so users with a local install never see the global command
  // promised when they're actually about to get a project-scoped one.
  const [installCommand, setInstallCommand] = useState<string>(
    "npm install -g wuphf@latest",
  );
  // silentUpTo: the "high water mark" version the user has muted up to. A
  // new release re-surfaces the banner only when there's a notable commit
  // between this and `latest` (or when the major segment bumps — see
  // `forceMajor` below). Read once on mount; updated by dismiss().
  const [silentUpTo, setSilentUpTo] = useState<string | null>(() =>
    safeLocalStorageGet(SILENT_UP_TO_KEY),
  );
  const [expanded, setExpanded] = useState(false);
  const [changelog, setChangelog] = useState<ChangelogState>({
    loading: false,
    error: null,
    commits: [],
    ready: false,
  });
  // Per-component latch so a successful (or non-abort-failed) fetch is
  // not retried when the user toggles expanded off and on again. Set in
  // the resolution callbacks (NOT at fetch-start) so a collapse-while-
  // loading leaves the ref unset and the next expand can retry. Keyed by
  // `${current}→${latest}` rather than a bare boolean so a future feature
  // that re-checks (e.g. periodic broker poll) and changes current/latest
  // re-triggers the fetch instead of silently rendering the stale cache.
  const changelogFetchedRef = useRef<string | null>(null);
  // Stable id so the toggle button's aria-controls can point at the
  // collapsible drawer for assistive tech.
  const changelogId = useId();

  // Run state: idle → running → done. The chip click flips to running;
  // the response (success/failure) sticks in `done` until the user
  // dismisses or reloads. There's no path back to idle — once you've
  // initiated an install, the next step is restart, not retry.
  const [run, setRun] = useState<RunState>({ phase: "idle" });

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
        setIsDevBuildSrv(!!res.is_dev_build);
        // Replace the chip label only when the server gave us a real
        // command — for "unknown" installs we keep the canonical
        // `npm install -g …` so the click outcome's "couldn't detect"
        // copy still makes sense alongside the chip.
        if (
          res.install_command &&
          res.install_method &&
          res.install_method !== "unknown"
        ) {
          setInstallCommand(res.install_command);
        }
      })
      .catch(() => {
        // Broker unreachable or returned a non-2xx — degrade silently.
      });
    return () => {
      ctl.abort();
    };
  }, [enabled, forced]);

  // Anchor the changelog fetch (and the notable-gate) on whichever is
  // newer between the silently-muted version and the running build. If
  // the user has muted up to v0.83.10 but is still on v0.83.7, "anything
  // notable since v0.83.10" is the right question — they explicitly told
  // us to wait until the diff exceeded their last seen state.
  const fromVersion = useMemo(() => {
    if (!current) return null;
    if (!silentUpTo) return current;
    if (!VERSION_RE.test(silentUpTo)) return current;
    return compareVersions(silentUpTo, current) > 0 ? silentUpTo : current;
  }, [current, silentUpTo]);

  // Eager changelog fetch (NOT gated on `expanded`) — we need the commits
  // to compute the notable-gate before deciding whether to render. The
  // fetch is broker-cached for an hour so this isn't expensive across
  // tab/page reloads. Re-fires whenever `from` or `latest` changes (e.g.
  // after the upgrade-check resolves on first mount).
  useEffect(() => {
    // Eager (NOT gated on `expanded`) — the notable-gate needs the
    // commits to decide whether to render the banner at all, before the
    // user has had a chance to expand. The broker caches the response
    // for an hour, so cost is bounded across tab/page reloads.
    if (!(fromVersion && latest)) return;
    if (compareVersions(fromVersion, latest) >= 0) {
      // Short-circuit (e.g. user just dismissed: silentUpTo == latest →
      // fromVersion == latest). Don't leave a stale "Loading changes…"
      // status if the user had the changelog expanded when they hit
      // dismiss — without this reset, the previous in-flight fetch's
      // loading=true survived the cleanup and the expanded panel kept
      // showing the loader forever.
      setChangelog({ loading: false, error: null, commits: [], ready: true });
      return;
    }
    // Per-component latch on `${fromVersion}→${latest}` so a successful
    // (or non-abort-failed) fetch is not retried when state churns and
    // the effect re-runs without the input pair actually changing. Set
    // in the resolution callbacks (NOT at fetch start) so an abort
    // leaves the ref unset and the next run can retry.
    const fetchKey = `${fromVersion}→${latest}`;
    if (changelogFetchedRef.current === fetchKey) return;
    const ctl = new AbortController();
    setChangelog({ loading: true, error: null, commits: [], ready: false });
    void get<UpgradeChangelogResponse>("/upgrade-changelog", {
      from: fromVersion,
      to: latest,
    })
      .then((data) => {
        if (ctl.signal.aborted) return;
        changelogFetchedRef.current = fetchKey;
        if (data.error) {
          // Failure-open: ready=true with empty commits would close the
          // gate. Mark ready=true so the gate decides "absence of
          // notable signal" but combine with `error` so the higher-level
          // decision can still failure-open if it wants to.
          setChangelog({
            loading: false,
            error: data.error,
            commits: [],
            ready: true,
          });
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
        setChangelog({
          loading: false,
          error: null,
          commits,
          ready: true,
        });
      })
      .catch((e: unknown) => {
        if (ctl.signal.aborted) return;
        // Latch on error too so the user sees the error message instead
        // of the next expand silently re-firing the same failing call.
        changelogFetchedRef.current = fetchKey;
        setChangelog({
          loading: false,
          error: e instanceof Error ? e.message : String(e),
          commits: [],
          ready: true,
        });
      });
    return () => {
      ctl.abort();
    };
  }, [fromVersion, latest]);

  const upgradeNeeded = useMemo(() => {
    if (!(current && latest)) return false;
    // Server flag is authoritative; the local check is the
    // URL-override fallback.
    if (isDevBuildSrv || isDevVersion(current)) return false;
    if (!(VERSION_RE.test(current) && VERSION_RE.test(latest))) return false;
    return compareVersions(current, latest) < 0;
  }, [current, latest, isDevBuildSrv]);

  // Major bump = first dotted segment differs between `from` and `latest`.
  // When true, the banner force-shows (bypasses dismiss) AND the visual
  // treatment escalates — major bumps are deliberate human decisions in
  // our auto-release setup, so they always deserve attention.
  const forceMajor = useMemo(() => {
    if (!(fromVersion && latest)) return false;
    return isMajorBump(fromVersion, latest);
  }, [fromVersion, latest]);

  // Notable-gate: hide the banner if we have a clean changelog with zero
  // feat/fix/perf/breaking commits. Failure-open in two cases:
  //   1. Changelog hasn't resolved yet (ready=false) — keep the banner
  //      hidden until we know, so docs-only patches don't briefly flash.
  //      But we ALSO use `compareUrl` etc. that need both versions; the
  //      enclosing `upgradeNeeded` already guards.
  //   2. Changelog errored OR returned an empty list with no error —
  //      treat as "we don't know what's in this release" → SHOW. Better
  //      to nag than to swallow a critical update because the GitHub
  //      compare API blipped.
  const notableGate = useMemo(() => {
    if (!changelog.ready) return false; // wait for resolution
    if (changelog.error) return true; // failure-open
    if (changelog.commits.length === 0) return true; // empty response: don't trust
    return hasNotable(changelog.commits);
  }, [changelog]);

  // Dismiss: silenced iff user explicitly muted up to (or past) the
  // current latest. A future release with a NEW notable commit will
  // re-shift `fromVersion` and re-evaluate the gate.
  const silenced = useMemo(() => {
    if (!latest) return false;
    if (!silentUpTo) return false;
    if (!VERSION_RE.test(silentUpTo)) return false;
    return compareVersions(silentUpTo, latest) >= 0;
  }, [silentUpTo, latest]);

  // compareUrl anchors on `fromVersion` (silentUpTo or current — whichever
  // is newer) so it matches the changelog list the user sees when they
  // expand "What's new". Anchoring on `current` would render
  // `<headline current → latest> + <list fromVersion..latest>`, which
  // confuses anyone who muted up past their installed version.
  const compareUrl = useMemo(() => {
    if (!(fromVersion && latest)) return "";
    return `https://github.com/${REPO}/compare/v${stripV(fromVersion)}...v${stripV(latest)}`;
  }, [fromVersion, latest]);

  const toggleExpanded = useCallback(() => {
    setExpanded((prev) => !prev);
  }, []);

  // Memoise the grouped commits so a render that doesn't change the
  // commit list (e.g. expand/collapse toggling) doesn't re-bucket.
  const grouped = useMemo(
    () => groupCommits(changelog.commits),
    [changelog.commits],
  );

  const dismiss = useCallback(() => {
    if (!latest) return;
    safeLocalStorageSet(SILENT_UP_TO_KEY, latest);
    setSilentUpTo(latest);
  }, [latest]);

  const triggerRun = useCallback(async () => {
    if (run.phase === "running") return;
    setRun({ phase: "running" });
    try {
      const result = await runUpgrade();
      setRun({ phase: "done", result });
    } catch (e: unknown) {
      // Network/timeout from the client side. Synthesise a result so the
      // UI has one shape to render against.
      setRun({
        phase: "done",
        result: {
          ok: false,
          install_method: "unknown",
          error: e instanceof Error ? e.message : String(e),
        },
      });
    }
  }, [run.phase]);

  const reload = useCallback(() => {
    window.location.reload();
  }, []);

  // Show/hide gate: full matrix lives in `decideShow` (utils) so the
  // logic is unit-testable without React. The carve-outs for run.phase
  // are encoded there:
  //   - phase==="running" pins the banner mounted regardless of dismiss.
  //   - phase==="done" defers to the same matrix as idle, so a user who
  //     hits dismiss after a failed install actually gets dismissed
  //     (instead of the banner sticking until reload).
  if (
    !decideShow({
      enabled,
      runPhase: run.phase,
      upgradeNeeded,
      forceMajor,
      silenced,
      notableGate,
    })
  ) {
    return null;
  }
  // upgradeNeeded already requires both current AND latest at runtime,
  // but the useMemo body isn't visible to TS's narrowing pass — keep
  // this guard so `current` / `latest` narrow from `string | null` to
  // `string` for the JSX below.
  if (!(current && latest)) return null;

  const bannerClass = `upgrade-banner${forceMajor ? " upgrade-banner--major" : ""}`;
  const runChipLabel = run.phase === "running" ? "Installing…" : null;

  return (
    // role="region" + an accessible name lets the banner be navigable as a
    // landmark without auto-announcing on every render the way role="status"
    // (a live region) would for what is really an interactive container.
    <div className={bannerClass} role="region" aria-label="Upgrade available">
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
            {forceMajor ? "Major update available: " : "Update available: "}
            <strong>v{stripV(fromVersion ?? current)}</strong> →{" "}
            <strong>v{stripV(latest)}</strong>
          </span>
          <button
            type="button"
            className="upgrade-banner-link"
            onClick={toggleExpanded}
            aria-expanded={expanded}
            aria-controls={changelogId}
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
          {run.phase === "done" ? (
            <UpgradeRunOutcome
              result={run.result}
              latest={stripV(latest)}
              onReload={reload}
            />
          ) : (
            <button
              type="button"
              className="upgrade-banner-run"
              onClick={() => {
                void triggerRun();
              }}
              disabled={run.phase === "running"}
              aria-busy={run.phase === "running"}
              title="Click to install"
            >
              {/* Play glyph mirrors InlineCommand's affordance so the
                  "click to execute" promise reads at-a-glance. */}
              <svg
                width="11"
                height="11"
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
                style={{ flexShrink: 0, opacity: 0.85 }}
              >
                <polygon points="6,4 20,12 6,20" />
              </svg>
              <code>{runChipLabel ?? installCommand}</code>
            </button>
          )}
          <button
            type="button"
            className="upgrade-banner-dismiss"
            onClick={dismiss}
            aria-label="Dismiss"
            disabled={forceMajor}
            title={forceMajor ? "Major updates can't be dismissed" : "Dismiss"}
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
      {/*
        Mounted unconditionally so the toggle's `aria-controls={changelogId}`
        always resolves to a real element — collapsed visibility is gated by
        the `hidden` attribute. The previous `{expanded && …}` form left
        aria-controls pointing at a missing node when collapsed, which the
        ARIA APG disclosure pattern warns AT support is inconsistent for.
        Children stay gated on `expanded` so the changelog fetch effect
        (line 122) keeps its fetch-on-expand semantics — nothing renders or
        triggers state subscriptions while collapsed.
      */}
      <div
        id={changelogId}
        className="upgrade-banner-changelog"
        hidden={!expanded}
      >
        {expanded && (
          <>
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
                      {entry.pr
                        ? (() => {
                            // Anchor only when prGitHubURL accepts the token —
                            // a non-numeric ref renders as muted plain text so
                            // it doesn't masquerade as a broken link.
                            const prURL = prGitHubURL(REPO, entry.pr);
                            return prURL ? (
                              <>
                                {" "}
                                <a
                                  href={prURL}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="upgrade-banner-pr"
                                >
                                  #{entry.pr}
                                </a>
                              </>
                            ) : (
                              <span className="upgrade-banner-pr-text">
                                {" "}
                                #{entry.pr}
                              </span>
                            );
                          })()
                        : null}
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </>
        )}
      </div>
    </div>
  );
}

// UpgradeRunOutcome renders the post-click result. Three states map to
// three visual treatments:
//   • ok=true               → "Installed vX.Y.Z. Restart to apply." + reload
//   • install_method=unknown → "Run `npm install -g …` from a terminal."
//   • everything else        → error message + the npm command they can copy
function UpgradeRunOutcome({
  result,
  latest,
  onReload,
}: {
  result: UpgradeRunResult;
  latest: string;
  onReload: () => void;
}) {
  const [showOutput, setShowOutput] = useState(false);
  if (result.ok) {
    return (
      <div className="upgrade-banner-outcome upgrade-banner-outcome--ok">
        <span>Installed v{latest}. Restart to apply.</span>
        <button type="button" className="upgrade-banner-run" onClick={onReload}>
          <code>Restart wuphf</code>
        </button>
      </div>
    );
  }
  return (
    <div className="upgrade-banner-outcome upgrade-banner-outcome--err">
      <span>
        {result.timed_out
          ? "Install timed out."
          : result.install_method === "unknown"
            ? "Couldn't detect install — run from a terminal:"
            : "Install failed:"}
        {result.error ? (
          <>
            {" "}
            <span className="upgrade-banner-outcome-msg">{result.error}</span>
          </>
        ) : null}
      </span>
      {result.command ? <code>{result.command}</code> : null}
      {result.output ? (
        <button
          type="button"
          className="upgrade-banner-link"
          onClick={() => setShowOutput((s) => !s)}
        >
          {showOutput ? "Hide output" : "Show output"}
        </button>
      ) : null}
      {showOutput && result.output ? (
        <pre className="upgrade-banner-output">{result.output}</pre>
      ) : null}
    </div>
  );
}
