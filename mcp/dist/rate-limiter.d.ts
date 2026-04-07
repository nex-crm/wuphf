export interface RateLimiterConfig {
    maxRequests: number;
    windowMs: number;
    dataDir: string;
}
export declare class RateLimiter {
    private config;
    private filePath;
    constructor(config?: Partial<RateLimiterConfig>);
    private readTimestamps;
    private writeTimestamps;
    canProceed(): boolean;
    recordRequest(): void;
}
//# sourceMappingURL=rate-limiter.d.ts.map