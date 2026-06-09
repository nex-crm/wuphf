import { Composer } from "../messages/Composer";
import { MessageFeed } from "../messages/MessageFeed";

interface TaskChannelChatProps {
  /**
   * The task's channel slug (`task-<id>`, or the owning channel for legacy /
   * system tasks). Passed explicitly because the task lives on the
   * `/tasks/$id` route, where `useChannelSlug()` returns null.
   */
  channel: string;
}

/**
 * TaskChannelChat — the task's real conversation surface.
 *
 * Every task owns a channel (`task-<id>`) whose default members are the owner,
 * the CEO, and the Librarian. This renders that channel's LIVE message stream
 * and the human's composer.
 *
 * It reuses the SAME shared chat primitives as the channel route — `MessageFeed`
 * (stream + the Office-phrase typing loader + threads + reactions + copy-link +
 * date separators + sender grouping + `/clear` marker) and `Composer`
 * (`@mentions`, `/slash` commands, the `@`/`/` autocomplete panel, and
 * Ctrl+P/N history recall) — passing the task's channel explicitly. An earlier
 * version reimplemented a thinner chat here (bare bubbles + a plain textarea),
 * which silently dropped the loader, mentions, slash commands, and threads, and
 * (via `MessageBubble`'s "general" fallback) posted reactions to the wrong
 * channel. Routing both through the shared, channel-prop-driven primitives
 * keeps the task chat and the channel chat at feature parity by construction.
 */
export function TaskChannelChat({ channel }: TaskChannelChatProps) {
  return (
    <section
      className="task-channel-chat conversation-chat"
      aria-label="Task channel conversation"
      data-testid="task-channel-chat"
    >
      <MessageFeed channel={channel} />
      <Composer channel={channel} />
    </section>
  );
}
