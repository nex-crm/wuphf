import type { DecisionPacket } from "../../lib/types/lifecycle";

interface PacketActionSidebarProps {
  packet: DecisionPacket;
  /** True during streaming/loading — disables decision actions. */
  isDecisionLocked: boolean;
  onMerge: () => void;
  onRequestChanges: () => void;
  onDefer: () => void;
  onBlock: () => void;
  onOpenInWorktree: () => void;
}

/**
 * Sticky right column of the Decision Packet view. Action button
 * hierarchy is locked by /plan-design-review:
 *  - Merge:      primary CTA (cyan accent, key `m`).
 *  - Request:    secondary (bg-card + strong border, key `r`).
 *  - Defer:      quiet (transparent + border, no keybind).
 *  - Block:      tertiary danger (transparent + red-500 border, key `b`).
 *  - Worktree:   quiet (transparent + border, key `w`).
 *
 * Disabled when the packet is in `streaming` / `loading` state. A11y:
 * `role="complementary"` so the right column reads as a sidebar
 * landmark, distinct from the center reading column.
 */
export function PacketActionSidebar({
  packet,
  isDecisionLocked,
  onMerge,
  onRequestChanges,
  onDefer,
  onBlock,
  onOpenInWorktree,
}: PacketActionSidebarProps) {
  const lockedTooltip = isDecisionLocked ? "Wait for review state" : undefined;
  const runtime = packet.sessionReport?.metadata?.runtime;
  const toolCalls = packet.sessionReport?.metadata?.tool_calls;
  const ownerSummary = runtime
    ? `${packet.ownerSlug} · ran ${runtime}${
        toolCalls ? ` · ${toolCalls} tool calls` : ""
      }`
    : packet.ownerSlug;

  const watchingValue = packet.reviewers
    .filter((r) => !r.isHuman)
    .map((r) => r.slug)
    .join(", ");

  return (
    <aside className="packet-right" aria-label="Decision actions">
      <h3>Decision</h3>
      <div className="packet-actions">
        <button
          type="button"
          className="packet-action packet-action--merge"
          onClick={onMerge}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Merge <span className="kbd">m</span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--secondary"
          onClick={onRequestChanges}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Request changes <span className="kbd">r</span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--quiet"
          onClick={onDefer}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Defer
          <span className="kbd" aria-hidden="true">
            ·
          </span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--danger"
          onClick={onBlock}
          disabled={isDecisionLocked}
          title={lockedTooltip}
        >
          Block <span className="kbd">b</span>
        </button>
        <button
          type="button"
          className="packet-action packet-action--quiet"
          onClick={onOpenInWorktree}
        >
          Open in worktree <span className="kbd">w</span>
        </button>
      </div>

      <h3>Context</h3>
      <div className="packet-aside-card">
        <div className="label">Owner agent</div>
        <div className="value">{ownerSummary}</div>
      </div>
      <div className="packet-aside-card">
        <div className="label">Worktree</div>
        <div className="value">
          <code>{packet.worktreePath}</code>
        </div>
      </div>
      {watchingValue ? (
        <div className="packet-aside-card">
          <div className="label">Watching</div>
          <div className="value">{watchingValue}</div>
        </div>
      ) : null}
      {packet.dependencies.blockedOn.length > 0 ? (
        <div className="packet-aside-card">
          <div className="label">Blocked on</div>
          <div className="value" style={{ color: "var(--warning-500)" }}>
            {packet.dependencies.blockedOn.join(", ")} — waiting merge
          </div>
        </div>
      ) : null}
    </aside>
  );
}
