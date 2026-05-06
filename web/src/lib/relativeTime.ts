/**
 * Format a millisecond delta into a compact relative time string.
 * Examples: "4s ago", "1m 30s ago", "2m ago".
 *
 * Designed for the Tier 2 hover peek recent-event list. Uses wall-clock
 * milliseconds rather than ISO strings to stay decoupled from server time.
 */
export function formatRelative(thenMs: number, nowMs: number): string {
  const diffMs = Math.max(0, nowMs - thenMs);
  const totalSeconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;

  if (minutes === 0) {
    return `${totalSeconds}s ago`;
  }
  if (seconds === 0) {
    return `${minutes}m ago`;
  }
  return `${minutes}m ${seconds}s ago`;
}
