import { useCallback, useMemo } from "react";

import type { WikiCatalogEntry } from "../../../../api/wiki";
import { uploadWikiFile, wikiFileUrl } from "../../../../api/wiki";
import { useAppStore } from "../../../../stores/app";
import { KBEditor } from "./editor";
import "./refclone.css";
import {
  type RefcloneEditorContextValue,
  RefcloneEditorProvider,
} from "./stores/editor-context";

/**
 * RefCloneEditor — WUPHF host wrapper around the ported reference KB editor.
 *
 * It mounts {@link RefcloneEditorProvider} (which bridges these props into the
 * editor's Zustand store shims) around the ported {@link KBEditor}, all inside a
 * `<div className="refclone-editor">` so `refclone.css`'s scoped design tokens
 * give the surface the reference LOOK. The editor itself never imports the wiki
 * API — every WUPHF-owned concern (content + persistence, file upload, wiki-link
 * resolution) crosses the context boundary through this wrapper.
 *
 * The drop-in replaces TiptapWikiEditor under WikiEditor: same `content`,
 * `onChange`, `resolver`, and `catalog` props.
 */
export interface RefCloneEditorProps {
  /** Markdown the editor edits. Pushed into the editor-store via the provider. */
  content: string;
  /** Persist edited markdown. Fired on every edit + source-mode apply. */
  onChange: (markdown: string) => void;
  /**
   * Resolve whether a wiki-link slug points at a real article. Used to derive a
   * catalog-backed slug → path resolver for in-editor wiki-link navigation.
   */
  resolver?: (slug: string) => boolean;
  /** Catalog the editor resolves wiki-links + mentions against. */
  catalog?: WikiCatalogEntry[];
}

/**
 * The editor renders its full chrome (toolbar, find bar, source mode) only when
 * `currentPath` is non-null. WikiEditor always opens against a concrete article,
 * so we feed a synthetic, stable in-editor path. It is never persisted — the
 * host owns the real article path through WikiEditor's controller — it only
 * keeps the editor out of its "No page selected" empty state and gives the
 * file-upload handlers a non-null page context to attach uploads to.
 */
const HOST_EDITOR_PATH = "team";

/** Build a slug → repo-root-relative path resolver from the wiki catalog. */
function buildWikiLinkResolver(
  catalog: WikiCatalogEntry[] | undefined,
  resolver: ((slug: string) => boolean) | undefined,
): (slug: string) => string | null {
  // Index catalog entries by their slug (last path segment, sans `.md`) so a
  // `[[slug]]` click resolves to the canonical article path.
  const bySlug = new Map<string, string>();
  if (catalog) {
    for (const entry of catalog) {
      const last = entry.path.split("/").pop() ?? entry.path;
      const slug = last.replace(/\.md$/i, "");
      if (!bySlug.has(slug)) bySlug.set(slug, entry.path);
    }
  }
  return (slug: string): string | null => {
    const exact = bySlug.get(slug);
    if (exact) return exact;
    // Fall back to the boolean resolver: if the host says the slug exists, hand
    // back the slug itself so navigation has a target to act on.
    if (resolver?.(slug)) return slug;
    return null;
  };
}

export default function RefCloneEditor({
  content,
  onChange,
  resolver,
  catalog,
}: RefCloneEditorProps) {
  // Dark palette: the reference editor reads its dark tokens from a `dark`
  // ancestor/own class. WUPHF drives theming via `data-theme` on the document;
  // mirror that here so the editor matches the host's light/dark surface.
  const theme = useAppStore((s) => s.theme);
  const isDark = theme !== "nex";

  const resolveWikiLink = useMemo(
    () => buildWikiLinkResolver(catalog, resolver),
    [catalog, resolver],
  );

  // Upload via WUPHF's multipart endpoint, then resolve to a servable URL.
  const uploadFile = useCallback(async (file: File): Promise<string | null> => {
    try {
      const res = await uploadWikiFile(HOST_EDITOR_PATH, file);
      return wikiFileUrl(res.path);
    } catch {
      return null;
    }
  }, []);

  const resolveAssetUrl = useCallback((path: string) => wikiFileUrl(path), []);

  const value = useMemo<RefcloneEditorContextValue>(
    () => ({
      currentPath: HOST_EDITOR_PATH,
      content,
      frontmatter: null,
      onChange,
      uploadFile,
      resolveWikiLink,
      resolveAssetUrl,
    }),
    [content, onChange, uploadFile, resolveWikiLink, resolveAssetUrl],
  );

  return (
    <div className={isDark ? "refclone-editor dark" : "refclone-editor"}>
      <RefcloneEditorProvider value={value}>
        <KBEditor />
      </RefcloneEditorProvider>
    </div>
  );
}
