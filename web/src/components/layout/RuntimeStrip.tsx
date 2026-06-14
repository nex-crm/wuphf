import { useOfficeStats } from "../../hooks/useOfficeStats";

/**
 * Thin strip under the channel header with pills for "N active",
 * "M blocked", "K need you". Mirrors the legacy runtime-strip.
 *
 * Counts come from the shared /office/stats hook — the same payload the
 * board lane headers, dashboard tiles, and inbox badge consume — so the
 * strip can never disagree with the board (the v1 "1 blocked vs Blocked
 * lane 0" drift came from this strip deriving `blocked` from a raw
 * status string while the board projected lifecycle stages).
 */
export function RuntimeStrip() {
  const { data: stats } = useOfficeStats();

  // No stats yet (first load or broker unreachable): render nothing
  // rather than claiming "all quiet" about a state we don't know.
  if (!stats) {
    return <div className="runtime-strip" />;
  }

  // "N active" counts working agents (as before); "M blocked" counts
  // board-blocked tasks; "K need you" counts pending blocking requests
  // (decision-lane tasks surface via the board's Needs-human lane and
  // the inbox badge — counting them here too would double-bill tasks
  // that also raised a request).
  const active = stats.agents_active;
  const blocked = stats.tasks.blocked;
  const needYou = stats.requests.blocking;

  if (active === 0 && blocked === 0 && needYou === 0) {
    return (
      <div className="runtime-strip">
        <span className="runtime-pill runtime-pill-idle">all quiet</span>
      </div>
    );
  }

  return (
    <div className="runtime-strip">
      {needYou > 0 && (
        <span className="runtime-pill runtime-pill-needyou">
          {needYou} need you
        </span>
      )}
      {active > 0 && (
        <span className="runtime-pill runtime-pill-active">
          {active} active
        </span>
      )}
      {blocked > 0 && (
        <span className="runtime-pill runtime-pill-blocked">
          {blocked} blocked
        </span>
      )}
    </div>
  );
}
