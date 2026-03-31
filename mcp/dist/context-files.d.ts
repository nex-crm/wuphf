import type { NexApiClient } from "./client.js";
import type { RateLimiter } from "./rate-limiter.js";
export interface ContextFilesResult {
    ingested: number;
    skipped: number;
    errors: number;
    files: string[];
}
export declare function ingestContextFiles(client: NexApiClient, rateLimiter: RateLimiter, cwd: string): Promise<ContextFilesResult>;
//# sourceMappingURL=context-files.d.ts.map