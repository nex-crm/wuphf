import type { NexApiClient } from "./client.js";
import type { RateLimiter } from "./rate-limiter.js";
export interface ScanConfig {
    enabled: boolean;
    extensions: string[];
    maxFileSize: number;
    maxFilesPerScan: number;
    scanDepth: number;
    ignoreDirs: string[];
}
export interface ScanResult {
    scanned: number;
    ingested: number;
    skipped: number;
    errors: number;
}
export declare function scanAndIngest(client: NexApiClient, rateLimiter: RateLimiter, cwd: string, config: ScanConfig): Promise<ScanResult>;
//# sourceMappingURL=file-scanner.d.ts.map