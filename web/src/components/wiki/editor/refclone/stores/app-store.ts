import { create } from "zustand";

/**
 * app-store shim for the refclone editor.
 *
 * The reference editor calls `useAppStore.getState().openTaskPanelCompose(...)`
 * from the "Edit with AI" affordance. WUPHF has no task-compose panel inside
 * the wiki editor, so this routes through an optional host callback and is a
 * no-op when the host doesn't supply one (per the integration contract).
 */

export interface ComposeRequest {
  source: string;
  pinnedPagePath: string | null;
  defaultAgentSlug: string;
}

export interface AppStore {
  openTaskPanelCompose: (req: ComposeRequest) => void;
  onCompose?: (req: ComposeRequest) => void;
}

export const useAppStore = create<AppStore>((set, get) => ({
  onCompose: undefined,
  openTaskPanelCompose: (req: ComposeRequest) => {
    get().onCompose?.(req);
  },
}));

export interface AppStoreBridge {
  onCompose?: (req: ComposeRequest) => void;
}

/** Push the host's compose callback into the store. */
export function bindAppStore(bridge: AppStoreBridge): void {
  useAppStore.setState({ onCompose: bridge.onCompose });
}
