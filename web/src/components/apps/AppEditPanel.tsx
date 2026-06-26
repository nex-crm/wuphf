import { Xmark } from "iconoir-react";

import { Composer } from "../messages/Composer";
import { MessageFeed } from "../messages/MessageFeed";

interface AppEditPanelProps {
  /** App name, shown in the panel header. */
  appName: string;
  /**
   * The app's persistent edit channel (`task-<id>` — the App Builder task that
   * created/improves it). The chat is bound to this slug, so the conversation
   * persists across sessions and is unique per app.
   */
  channel: string;
  onClose: () => void;
}

/**
 * AppEditPanel — the per-app "chat to edit" side rail (v0/Cursor-style).
 *
 * It is NOT a new chat system: it mounts the SAME shared channel primitives the
 * rest of the app uses — `MessageFeed` (stream + typing loader + threads +
 * reactions + mentions + slash commands) and `Composer` — bound to the app's
 * persistent edit channel. A human edit request ("add a CSV export button")
 * posts there like any message; the broker's existing follow-up wake re-engages
 * the App Builder, which reads the current source (get_app) and republishes
 * (register_app). The App Builder's narration streams back into this feed, and
 * the live preview in the main stage hot-reloads when the app republishes.
 *
 * The conversation is the app's full build/edit history, so the human can scroll
 * back through every prior change request and the agent's responses.
 */
export function AppEditPanel({ appName, channel, onClose }: AppEditPanelProps) {
  return (
    <aside
      className="app-edit-panel conversation-chat"
      aria-label={`Edit ${appName} with the App Builder`}
      data-testid="app-edit-panel"
    >
      <header className="app-edit-panel__header">
        <div className="app-edit-panel__heading">
          <span className="app-edit-panel__eyebrow">App Builder</span>
          <h2 className="app-edit-panel__title">Editing {appName}</h2>
        </div>
        <button
          type="button"
          className="app-edit-panel__close"
          aria-label="Close edit chat"
          onClick={onClose}
        >
          <Xmark width={16} height={16} />
        </button>
      </header>
      <MessageFeed channel={channel} />
      <Composer channel={channel} />
    </aside>
  );
}
