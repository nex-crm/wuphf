/**
 * TickManager: drives agent loops on a fixed interval.
 * Each agent gets its own setInterval; ticks are skipped
 * when the agent is idle with no queued work.
 */

export class TickManager {
  private intervals = new Map<string, ReturnType<typeof setInterval>>();
  private tickRateMs: number;

  constructor(tickRateMs = 500) {
    this.tickRateMs = tickRateMs;
  }

  startLoop(
    slug: string,
    loop: { getState(): { phase: string }; start(): void; tick(): Promise<void> },
    hasWork: () => boolean,
  ): void {
    if (this.intervals.has(slug)) return; // idempotent

    const interval = setInterval(async () => {
      const state = loop.getState();
      // Only tick if there's pending work
      if (
        (state.phase === 'done' || state.phase === 'error' || state.phase === 'idle') &&
        !hasWork()
      ) {
        return; // idle skip
      }
      if (state.phase === 'done' || state.phase === 'error') {
        loop.start(); // reset to idle
      }
      try {
        await loop.tick();
      } catch {
        /* swallow tick errors */
      }
    }, this.tickRateMs);

    this.intervals.set(slug, interval);
  }

  stopLoop(slug: string): void {
    const interval = this.intervals.get(slug);
    if (interval) {
      clearInterval(interval);
      this.intervals.delete(slug);
    }
  }

  isRunning(slug: string): boolean {
    return this.intervals.has(slug);
  }

  stopAll(): void {
    for (const [slug] of this.intervals) {
      this.stopLoop(slug);
    }
  }
}
