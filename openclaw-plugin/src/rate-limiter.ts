/**
 * Sliding window rate limiter with async queue.
 * Designed for WUPHF /text endpoint (10 req/min).
 */

export interface RateLimiterConfig {
  maxRequests: number;
  windowMs: number;
  maxQueueDepth: number;
}

const DEFAULTS: RateLimiterConfig = {
  maxRequests: 10,
  windowMs: 60_000,
  maxQueueDepth: 5,
};

interface QueuedRequest {
  fn: () => Promise<void>;
  resolve: () => void;
  reject: (err: Error) => void;
}

export class RateLimiter {
  private timestamps: number[] = [];
  private queue: QueuedRequest[] = [];
  private draining = false;
  private config: RateLimiterConfig;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(config?: Partial<RateLimiterConfig>) {
    this.config = { ...DEFAULTS, ...config };
  }

  /** Enqueue a function to be executed within rate limits. Fire-and-forget. */
  enqueue(fn: () => Promise<void>): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      this.queue.push({ fn, resolve, reject });

      // LIFO eviction: if queue exceeds max depth, drop oldest (front)
      while (this.queue.length > this.config.maxQueueDepth) {
        const dropped = this.queue.shift()!;
        dropped.reject(new Error("Rate limiter queue full — request dropped (LIFO eviction)"));
      }

      this.drain();
    });
  }

  private canProceed(): boolean {
    const now = Date.now();
    // Remove timestamps outside the window
    this.timestamps = this.timestamps.filter((t) => now - t < this.config.windowMs);
    return this.timestamps.length < this.config.maxRequests;
  }

  private msUntilSlot(): number {
    if (this.timestamps.length === 0) return 0;
    const oldest = this.timestamps[0];
    return Math.max(0, this.config.windowMs - (Date.now() - oldest));
  }

  private async drain(): Promise<void> {
    if (this.draining) return;
    this.draining = true;

    try {
      while (this.queue.length > 0) {
        if (this.canProceed()) {
          const item = this.queue.shift()!;
          this.timestamps.push(Date.now());
          try {
            await item.fn();
            item.resolve();
          } catch (err) {
            item.reject(err instanceof Error ? err : new Error(String(err)));
          }
        } else {
          // Wait until a slot opens
          const wait = this.msUntilSlot();
          await new Promise<void>((r) => {
            this.timer = setTimeout(r, wait);
          });
        }
      }
    } finally {
      this.draining = false;
    }
  }

  /** Flush pending queue (called on shutdown). */
  async flush(): Promise<void> {
    // Let current drain finish
    while (this.draining || this.queue.length > 0) {
      await new Promise((r) => setTimeout(r, 50));
    }
  }

  /** Cancel all pending and stop timers. */
  destroy(): void {
    if (this.timer) clearTimeout(this.timer);
    for (const item of this.queue) {
      item.reject(new Error("Rate limiter destroyed"));
    }
    this.queue.length = 0;
    this.timestamps.length = 0;
  }

  /** Current queue depth (for debugging). */
  get pending(): number {
    return this.queue.length;
  }
}
