import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join, dirname } from "node:path";
const CONFIG_PATH = join(homedir(), ".wuphf", "config.json");
export function loadConfig() {
    try {
        const raw = readFileSync(CONFIG_PATH, "utf-8");
        return JSON.parse(raw);
    }
    catch {
        return {};
    }
}
export function saveConfig(config) {
    mkdirSync(dirname(CONFIG_PATH), { recursive: true });
    writeFileSync(CONFIG_PATH, JSON.stringify(config, null, 2) + "\n", "utf-8");
}
export function loadApiKey() {
    return process.env.WUPHF_API_KEY || loadConfig().api_key || undefined;
}
export function persistRegistration(data) {
    const existing = loadConfig();
    if (typeof data.api_key === "string")
        existing.api_key = data.api_key;
    if (typeof data.workspace_id === "string" || typeof data.workspace_id === "number") {
        existing.workspace_id = String(data.workspace_id);
    }
    if (typeof data.workspace_slug === "string")
        existing.workspace_slug = data.workspace_slug;
    saveConfig(existing);
}
export { CONFIG_PATH };
//# sourceMappingURL=config.js.map