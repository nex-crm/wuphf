import { z } from "zod";
import { RateLimiter } from "../rate-limiter.js";
import { scanAndIngest } from "../file-scanner.js";
import { ingestContextFiles } from "../context-files.js";
const rateLimiter = new RateLimiter();
export function registerScanTools(server, client) {
    server.tool("scan_files", "Scan a directory for text files and ingest changed ones into the WUPHF knowledge base. Uses manifest-based change detection — unchanged files are skipped automatically.", {
        directory: z.string().optional().describe("Directory to scan (default: current working directory)"),
        max_files: z.number().optional().describe("Maximum files to ingest per scan (default: 5)"),
        max_depth: z.number().optional().describe("Maximum directory depth (default: 2)"),
    }, { readOnlyHint: false }, async ({ directory, max_files, max_depth }) => {
        const scanConfig = {
            enabled: true,
            extensions: [".md", ".txt", ".csv", ".json", ".yaml", ".yml"],
            maxFileSize: 100_000,
            maxFilesPerScan: max_files ?? 5,
            scanDepth: max_depth ?? 2,
            ignoreDirs: ["node_modules", ".git", "dist", "build", ".next", "__pycache__", "vendor", ".venv", ".claude", "coverage", ".turbo", ".cache"],
        };
        const cwd = directory || process.cwd();
        const result = await scanAndIngest(client, rateLimiter, cwd, scanConfig);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("ingest_context_files", "Ingest CLAUDE.md and memory files from the current project into WUPHF. Uses manifest-based change detection.", {
        directory: z.string().optional().describe("Project directory (default: current working directory)"),
    }, { readOnlyHint: false }, async ({ directory }) => {
        const cwd = directory || process.cwd();
        const result = await ingestContextFiles(client, rateLimiter, cwd);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=scan.js.map