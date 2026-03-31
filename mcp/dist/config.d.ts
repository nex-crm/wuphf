declare const CONFIG_PATH: string;
interface NexMcpConfig {
    api_key?: string;
    base_url?: string;
    dev_url?: string;
    workspace_id?: string;
    workspace_slug?: string;
    [key: string]: unknown;
}
export declare function loadConfig(): NexMcpConfig;
export declare function saveConfig(config: NexMcpConfig): void;
export declare function loadApiKey(): string | undefined;
export declare function persistRegistration(data: Record<string, unknown>): void;
export { CONFIG_PATH };
//# sourceMappingURL=config.d.ts.map