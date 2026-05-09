// AGENTS.md rule 11: this is the monotonic-clock surface for src/main/.
// Wall-clock Date APIs remain forbidden.
export function monotonicNowMs(): number {
  return performance.now();
}
