export interface SessionStoreConfig {
    maxSize: number;
    dataDir: string;
}
export declare class SessionStore {
    private filePath;
    private maxSize;
    constructor(config?: Partial<SessionStoreConfig>);
    private readStore;
    private writeStore;
    get(sessionKey: string): string | undefined;
    set(sessionKey: string, nexSessionId: string): void;
    delete(sessionKey: string): boolean;
    get size(): number;
    clear(): void;
}
//# sourceMappingURL=session-store.d.ts.map