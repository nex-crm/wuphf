/**
 * In-memory session store mapping OpenClaw sessionKeys to Nex session IDs.
 * LRU eviction at configurable max size.
 */

const DEFAULT_MAX = 1000;

export class SessionStore {
  private map = new Map<string, string>();
  private maxSize: number;

  constructor(maxSize = DEFAULT_MAX) {
    this.maxSize = maxSize;
  }

  get(sessionKey: string): string | undefined {
    const value = this.map.get(sessionKey);
    if (value !== undefined) {
      // Move to end (most recently used)
      this.map.delete(sessionKey);
      this.map.set(sessionKey, value);
    }
    return value;
  }

  set(sessionKey: string, nexSessionId: string): void {
    // If already exists, delete first to update position
    if (this.map.has(sessionKey)) {
      this.map.delete(sessionKey);
    }

    this.map.set(sessionKey, nexSessionId);

    // LRU eviction
    while (this.map.size > this.maxSize) {
      const oldest = this.map.keys().next().value!;
      this.map.delete(oldest);
    }
  }

  delete(sessionKey: string): boolean {
    return this.map.delete(sessionKey);
  }

  get size(): number {
    return this.map.size;
  }

  clear(): void {
    this.map.clear();
  }
}
