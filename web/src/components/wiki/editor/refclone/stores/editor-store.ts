import { create } from "zustand";

/**
 * editor-store shim for the refclone editor.
 *
 * The reference editor (editor.tsx, editor-toolbar.tsx, slash-commands.tsx,
 * folder-index.tsx) is coupled to an `@/stores/editor-store` Zustand store that
 * owns the current page's markdown, frontmatter, load/save status, and the
 * load/save/create lifecycle. In the reference app that store talks directly to
 * a `/api` backend.
 *
 * In WUPHF the host controller (the RefcloneEditor wrapper) owns the content and
 * persistence. To keep the ported editor files byte-for-byte verbatim we expose
 * the SAME store API here, but make it a thin bridge:
 *
 *   - `content` / `currentPath` / `frontmatter` / status flags are pushed IN
 *     from React props via {@link bindEditorStore}, called by the provider in
 *     editor-context.tsx whenever the host's props change.
 *   - `updateContent` routes OUT through an injected `onChange` callback so the
 *     host persists. It also mirrors the value locally + flips `isDirty` so the
 *     editor's render-dedupe effect behaves exactly as upstream.
 *   - `updateFrontmatter` patches the local frontmatter (drives RTL toggling)
 *     and routes through `onChange` so the change is not lost.
 *   - `loadPage` routes through an injected `onNavigate` callback (the host
 *     drives routing/fetch); it is otherwise a no-op here.
 *   - `createMissingPage` routes through an injected `onCreatePage` callback.
 *
 * Keeping this a real Zustand store (not a context-only value) is deliberate:
 * the editor files use BOTH the hook form `useEditorStore((s) => s.content)`
 * AND the imperative form `useEditorStore.getState().updateContent(md)` inside
 * ProseMirror event handlers. Only a store gives both verbatim.
 */

export type SaveStatus = "idle" | "saving" | "saved" | "error";
export type LoadStatus = "idle" | "loading" | "ok" | "missing" | "error";

export interface FrontMatter {
  /** Document reading direction. `"rtl"` flips the editor + per-block flow. */
  dir?: "rtl" | "ltr";
  /** Display title; surfaced by folder-index entries. */
  title?: string;
  [key: string]: unknown;
}

/**
 * Host-supplied callbacks. The provider wires these from the RefcloneEditor
 * props/context so the store stays decoupled from any concrete transport.
 */
export interface EditorStoreBridge {
  /** Persist new markdown. Called on every editor edit + source-mode apply. */
  onChange?: (markdown: string) => void;
  /** Open another page (wiki-link / internal-link navigation). */
  onNavigate?: (path: string) => void;
  /** Create the index page for a folder that has no index yet. */
  onCreatePage?: (title: string) => void | Promise<void>;
}

export interface EditorStore extends EditorStoreBridge {
  currentPath: string | null;
  content: string;
  frontmatter: FrontMatter | null;
  saveStatus: SaveStatus;
  loadStatus: LoadStatus;
  isLoading: boolean;
  isDirty: boolean;

  loadPage: (path: string) => Promise<void>;
  createMissingPage: (title: string) => Promise<void>;
  updateContent: (content: string) => void;
  updateFrontmatter: (updates: Partial<FrontMatter>) => void;
}

export const useEditorStore = create<EditorStore>((set, get) => ({
  currentPath: null,
  content: "",
  frontmatter: null,
  saveStatus: "idle",
  loadStatus: "idle",
  isLoading: false,
  isDirty: false,

  onChange: undefined,
  onNavigate: undefined,
  onCreatePage: undefined,

  loadPage: async (path: string) => {
    // The host drives routing + fetch; we only forward the intent. The new
    // page's content/path arrive back through bindEditorStore from the
    // provider once the host has loaded it.
    get().onNavigate?.(path);
  },

  createMissingPage: async (title: string) => {
    await get().onCreatePage?.(title);
  },

  updateContent: (content: string) => {
    set({ content, isDirty: true });
    get().onChange?.(content);
  },

  updateFrontmatter: (updates: Partial<FrontMatter>) => {
    const { frontmatter } = get();
    const next: FrontMatter = { ...(frontmatter ?? {}), ...updates };
    set({ frontmatter: next, isDirty: true });
  },
}));

/**
 * Push host props into the store. Called by the provider on every prop change.
 *
 * `isDirty` is reset to false on a real (path, content) change so the editor's
 * render-dedupe effect re-renders the incoming markdown. A pure re-bind with
 * the same path + content leaves `isDirty` untouched so an in-flight edit isn't
 * clobbered.
 */
export function bindEditorStore(next: {
  currentPath: string | null;
  content: string;
  frontmatter: FrontMatter | null;
  saveStatus: SaveStatus;
  loadStatus: LoadStatus;
  isLoading: boolean;
  bridge: EditorStoreBridge;
}): void {
  const prev = useEditorStore.getState();
  const pathChanged = prev.currentPath !== next.currentPath;
  const contentChanged = prev.content !== next.content;

  useEditorStore.setState({
    currentPath: next.currentPath,
    content: next.content,
    frontmatter: next.frontmatter,
    saveStatus: next.saveStatus,
    loadStatus: next.loadStatus,
    isLoading: next.isLoading,
    isDirty: pathChanged || contentChanged ? false : prev.isDirty,
    onChange: next.bridge.onChange,
    onNavigate: next.bridge.onNavigate,
    onCreatePage: next.bridge.onCreatePage,
  });
}
