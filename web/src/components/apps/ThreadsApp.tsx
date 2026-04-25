import { useQueries, useQuery } from "@tanstack/react-query";

import { getChannels, getMessages, type Message } from "../../api/client";
import { useOfficeMembers } from "../../hooks/useMembers";
import { formatRelativeTime } from "../../lib/format";
import { useAppStore } from "../../stores/app";
import { PixelAvatar } from "../ui/PixelAvatar";

interface ThreadRow {
  id: string;
  channel: string;
  message: Message;
  replyCount: number;
}

/**
 * All-threads surface (legacy `openThreadsView`). Walks every channel's
 * recent messages, keeps the ones with thread_count > 0, and sorts by
 * reply count so the loudest conversations surface first.
 */
export function ThreadsApp() {
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel);
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId);
  const setLastMessageId = useAppStore((s) => s.setLastMessageId);
  const { data: members = [] } = useOfficeMembers();

  const { data: channelsData } = useQuery({
    queryKey: ["channels"],
    queryFn: () => getChannels(),
  });
  const channels = channelsData?.channels ?? [];

  const channelQueries = useQueries({
    queries: channels.map((ch) => ({
      queryKey: ["messages", ch.slug, "all"],
      queryFn: () => getMessages(ch.slug, null, 100),
      refetchInterval: 20_000,
    })),
  });

  const loading = channelQueries.some((q) => q.isLoading);
  const threads: ThreadRow[] = [];
  channelQueries.forEach((q, idx) => {
    const slug = channels[idx]?.slug;
    if (!(slug && q.data?.messages)) return;
    for (const msg of q.data.messages) {
      if ((msg.thread_count ?? 0) > 0) {
        threads.push({
          id: msg.id,
          channel: slug,
          message: msg,
          replyCount: msg.thread_count ?? 0,
        });
      }
    }
  });
  threads.sort((a, b) => b.replyCount - a.replyCount);

  function openThread(t: ThreadRow) {
    setCurrentApp(null);
    setCurrentChannel(t.channel);
    setLastMessageId(null);
    setActiveThreadId(t.id);
  }

  return (
    <div className="threads-view">
      <div className="threads-view-header">
        <span className="threads-view-title">Threads</span>
        <span className="threads-view-count">
          {threads.length} active thread{threads.length === 1 ? "" : "s"}
        </span>
      </div>

      {loading && threads.length === 0 ? (
        <div className="threads-view-empty">Loading threads...</div>
      ) : threads.length === 0 ? (
        <div className="threads-view-empty">
          No threads yet. Reply to a message to start one.
        </div>
      ) : (
        <div className="threads-view-list">
          {threads.map((t) => {
            const agent = members.find((m) => m.slug === t.message.from);
            const preview =
              t.message.content && t.message.content.length > 120
                ? `${t.message.content.slice(0, 120)}\u2026`
                : t.message.content || "(no content)";
            return (
              <button
                key={`${t.channel}-${t.id}`}
                type="button"
                className="thread-list-item"
                onClick={() => openThread(t)}
              >
                <div className="thread-list-item-avatar">
                  {agent ? (
                    <PixelAvatar slug={agent.slug} size={32} />
                  ) : (
                    <span style={{ fontSize: 22 }}>{"\uD83D\uDCAC"}</span>
                  )}
                </div>
                <div className="thread-list-item-body">
                  <div className="thread-list-item-preview">{preview}</div>
                  <div className="thread-list-item-meta">
                    <span className="thread-list-item-replies">
                      {t.replyCount} repl{t.replyCount === 1 ? "y" : "ies"}
                    </span>
                    {agent && <span>{agent.name}</span>}
                    <span>#{t.channel}</span>
                    {t.message.timestamp && (
                      <span>{formatRelativeTime(t.message.timestamp)}</span>
                    )}
                  </div>
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
