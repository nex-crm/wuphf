import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
} from "react";

import type { TreeNode } from "../lib/tree";
import {
  type AppStoreBridge,
  bindAppStore,
  type ComposeRequest,
} from "./app-store";
import {
  bindEditorStore,
  type EditorStoreBridge,
  type FrontMatter,
  type LoadStatus,
  type SaveStatus,
} from "./editor-store";
import { bindTreeStore, type TreeStoreBridge } from "./tree-store";

/**
 * The contract the RefcloneEditor wrapper (built in the Integrate phase) must
 * satisfy. Everything the ported editor needs that WUPHF owns flows through
 * this single context: content + persistence, the page tree + navigation,
 * upload, and wiki-link resolution.
 *
 * The reference editor's stores are bridged to these props by
 * {@link RefcloneEditorProvider} — see the per-store shims for the mapping.
 */
export interface RefcloneEditorContextValue {
  // ── Page content + persistence (→ editor-store) ──────────────────────────
  /** Repo-root-relative path of the open page, or null when none is selected. */
  currentPath: string | null;
  /** Current markdown body of the open page. */
  content: string;
  /** Parsed frontmatter; `dir: "rtl"` flips the editor. Null when none. */
  frontmatter: FrontMatter | null;
  /** Persist edited markdown. Fired on every edit + source-mode apply. */
  onChange: (markdown: string) => void;
  /** Whether a load is in flight (drives the loading overlay). */
  isLoading?: boolean;
  /** Distinguishes "missing index" from "ok"/"error" — drives the create CTA. */
  loadStatus?: LoadStatus;
  /** Surfaced in the status bar (Saving…/Saved/Save failed). */
  saveStatus?: SaveStatus;
  /** Create the index page for a folder with no index yet. */
  onCreatePage?: (title: string) => void | Promise<void>;

  // ── Page tree + navigation (→ tree-store) ────────────────────────────────
  /** The wiki page tree the editor resolves links + mentions against. */
  nodes?: TreeNode[];
  /** Open another page (wiki-link / internal-link / folder-index click). */
  onNavigate?: (path: string) => void;
  /** Expand an ancestor folder so the selected page shows in the host tree. */
  onExpandPath?: (path: string) => void;

  // ── Upload (→ injected into the editor file handlers) ────────────────────
  /**
   * Upload a pasted/dropped/picked file for the open page and return a
   * servable URL to embed, or null on failure. The wrapper wires this to
   * WUPHF's POST /wiki/upload (uploadWikiFile) + wikiFileUrl.
   */
  uploadFile: (file: File) => Promise<string | null>;

  // ── Wiki-link resolution (→ injected resolver) ───────────────────────────
  /**
   * Resolve a wiki-link slug to a target page path, or null when broken.
   * Optional — when omitted the editor falls back to its built-in
   * tree-based slug lookup.
   */
  resolveWikiLink?: (slug: string) => string | null;

  /**
   * Build a servable URL for a repo-root-relative asset path (e.g. a gallery
   * thumbnail in the folder index). The wrapper wires this to WUPHF's
   * wikiFileUrl. Optional — when omitted the raw path is used as-is.
   */
  resolveAssetUrl?: (path: string) => string;

  // ── AI compose (→ app-store) ─────────────────────────────────────────────
  /** Optional handler for the "Edit with AI" affordance. No-op when absent. */
  onCompose?: (req: ComposeRequest) => void;
}

const RefcloneEditorContext = createContext<RefcloneEditorContextValue | null>(
  null,
);

/**
 * Read the editor context. Throws when used outside the provider so wiring
 * mistakes surface immediately rather than as a silent no-op upload.
 */
export function useRefcloneEditorContext(): RefcloneEditorContextValue {
  const ctx = useContext(RefcloneEditorContext);
  if (!ctx) {
    throw new Error(
      "useRefcloneEditorContext must be used within a RefcloneEditorProvider",
    );
  }
  return ctx;
}

/**
 * Provider that bridges WUPHF props into the ported editor's Zustand store
 * shims AND exposes the upload + wiki-link resolver via context. Mount this
 * once around the ported `KBEditor`.
 */
export function RefcloneEditorProvider({
  value,
  children,
}: {
  value: RefcloneEditorContextValue;
  children: ReactNode;
}): ReactNode {
  const editorBridge = useMemo<EditorStoreBridge>(
    () => ({
      onChange: value.onChange,
      onNavigate: value.onNavigate,
      onCreatePage: value.onCreatePage,
    }),
    [value.onChange, value.onNavigate, value.onCreatePage],
  );

  const treeBridge = useMemo<TreeStoreBridge>(
    () => ({
      onSelectPage: value.onNavigate,
      onExpandPath: value.onExpandPath,
    }),
    [value.onNavigate, value.onExpandPath],
  );

  const appBridge = useMemo<AppStoreBridge>(
    () => ({ onCompose: value.onCompose }),
    [value.onCompose],
  );

  // Push props into the store shims synchronously on every change. Done in an
  // effect (not during render) because the stores are module-level singletons
  // and setState during render would warn under React 19's StrictMode.
  useEffect(() => {
    bindEditorStore({
      currentPath: value.currentPath,
      content: value.content,
      frontmatter: value.frontmatter,
      saveStatus: value.saveStatus ?? "idle",
      loadStatus: value.loadStatus ?? "ok",
      isLoading: value.isLoading ?? false,
      bridge: editorBridge,
    });
  }, [
    value.currentPath,
    value.content,
    value.frontmatter,
    value.saveStatus,
    value.loadStatus,
    value.isLoading,
    editorBridge,
  ]);

  useEffect(() => {
    bindTreeStore({ nodes: value.nodes ?? [], bridge: treeBridge });
  }, [value.nodes, treeBridge]);

  useEffect(() => {
    bindAppStore(appBridge);
  }, [appBridge]);

  return (
    <RefcloneEditorContext.Provider value={value}>
      {children}
    </RefcloneEditorContext.Provider>
  );
}
