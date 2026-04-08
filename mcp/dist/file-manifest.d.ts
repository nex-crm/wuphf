/**
 * Persistent file manifest — tracks which files have been ingested
 * using mtime + size as change detection.
 *
 * Stored at ~/.wuphf/file-scan-manifest.json.
 */
import { type Stats } from "node:fs";
export interface FileManifestEntry {
    mtime: number;
    size: number;
    ingestedAt: number;
    context: string;
}
export interface FileManifest {
    version: 1;
    files: Record<string, FileManifestEntry>;
}
export declare function readManifest(): FileManifest;
export declare function writeManifest(manifest: FileManifest): void;
export declare function isChanged(path: string, stat: Stats, manifest: FileManifest): boolean;
export declare function markIngested(path: string, stat: Stats, context: string, manifest: FileManifest): void;
//# sourceMappingURL=file-manifest.d.ts.map