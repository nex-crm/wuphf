export declare class NexApiError extends Error {
    status: number;
    statusText: string;
    body: unknown;
    constructor(status: number, statusText: string, body: unknown);
}
export declare class NexApiClient {
    private apiKey;
    constructor(apiKey?: string);
    get isAuthenticated(): boolean;
    setApiKey(key: string): void;
    private requireAuth;
    private request;
    register(email: string, name?: string, companyName?: string, source?: string): Promise<unknown>;
    get(path: string): Promise<unknown>;
    post(path: string, body?: unknown): Promise<unknown>;
    put(path: string, body?: unknown): Promise<unknown>;
    patch(path: string, body?: unknown): Promise<unknown>;
    delete(path: string): Promise<unknown>;
    getRaw(path: string): Promise<string>;
    private getBaseUrl;
}
//# sourceMappingURL=client.d.ts.map