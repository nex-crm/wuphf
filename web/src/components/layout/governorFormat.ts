import type { GovernorReason, GovernorStatus } from "../../api/governor";

/** "152k" / "980" — compact token count for chips and banners. */
export function formatTokens(n: number): string {
  if (n >= 1000) {
    return `${Math.round(n / 1000)}k`;
  }
  return `${Math.max(0, Math.round(n))}`;
}

/** "$2.10" — cost with two decimals; "$0.00" when unknown. */
export function formatCost(n: number): string {
  return `$${Math.max(0, n).toFixed(2)}`;
}

/** One-line reason headline for the paused banner. */
export function reasonHeadline(reason: GovernorReason): string {
  switch (reason) {
    case "budget":
      return "Budget checkpoint";
    case "turns":
      return "Review checkpoint";
    case "stop":
      return "Stopped";
    case "manual":
      return "Paused";
    default:
      return "Paused";
  }
}

/** Human sentence describing what tripped the pause. */
export function reasonDetail(status: GovernorStatus): string {
  const tokens = formatTokens(status.tokensSinceCheckpoint);
  const cost = formatCost(status.costSinceCheckpoint);
  const turns = status.turnsSinceCheckpoint;
  switch (status.reason) {
    case "budget":
      return `The team has used ${tokens} tokens (${cost}) since the last checkpoint. Review the work, then continue or stop.`;
    case "turns":
      return `The team has run ${turns} turns without a human in the loop. Review the work, then continue or stop.`;
    case "stop":
      return "In-flight work was cancelled. Resume when you are ready.";
    default:
      return "Dispatch is paused. Review the work, then continue or stop.";
  }
}

/** Compact "12 turns · 152k tok · $2.10" meter for the status bar. */
export function meterSummary(status: GovernorStatus): string {
  const turns = status.turnsSinceCheckpoint;
  const tokens = formatTokens(status.tokensSinceCheckpoint);
  const cost = formatCost(status.costSinceCheckpoint);
  return `${turns} ${turns === 1 ? "turn" : "turns"} · ${tokens} tok · ${cost}`;
}
