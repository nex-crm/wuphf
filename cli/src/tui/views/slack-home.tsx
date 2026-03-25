/**
 * SlackHome — the main Slack-style home view.
 *
 * Connects all Slack components to services:
 * - Sidebar with DMs (agents) and channels from ChatService
 * - Message list with grouping from ChatService
 * - Thread panel for in-thread replies
 * - Quick switcher overlay (Ctrl+K)
 * - Compose area with slash commands and @mentions
 * - Agent routing: DM → steer(), channel → broadcast
 */

import React, { useState, useEffect, useMemo, useCallback } from "react";
import { useStdout } from "ink";
import { SlackLayout, computeLayout } from "../components/slack/layout.js";
import { SlackSidebar } from "../components/slack/sidebar.js";
import type { SidebarSectionData, SidebarItemData } from "../components/slack/sidebar-types.js";
import { SlackMessageList } from "../components/slack/messages.js";
import type { ChatMessageInput } from "../components/slack/messages.js";
import { ComposeArea } from "../components/slack/compose.js";
import { ThreadPanel } from "../components/slack/thread-panel.js";
import type { ThreadMessage } from "../components/slack/thread-panel.js";
import { QuickSwitcher } from "../components/slack/quick-switcher.js";
import type { QuickSwitcherItem } from "../components/slack/quick-switcher.js";
import { ChannelHeader } from "./slack-channel-header.js";
import { getChatService } from "../services/chat-service.js";
import { getAgentService } from "../services/agent-service.js";
import {
  parseSlashInput,
  getSlashCommand,
  listSlashCommands,
  getInitState,
  handleInitInput,
  getAgentWizardState,
  handleAgentWizardInput,
  openAgentsManager,
} from "../slash-commands.js";
import type { ConversationMessage, SlashCommandContext } from "../slash-commands.js";
import type { SelectOption } from "../components/inline-select.js";
import { dispatch } from "../../commands/dispatch.js";
import { resolveApiKey } from "../../lib/config.js";
import { InlineSelect } from "../components/inline-select.js";
import { InlineConfirm } from "../components/inline-confirm.js";
import { Spinner } from "../components/spinner.js";

// ── Types ─────────────────────────────────────────────────────────

type FocusSection = "sidebar" | "messages" | "compose" | "thread";

interface PickerState {
  title: string;
  options: SelectOption[];
  onSelect: (value: string) => void;
}

interface ConfirmState {
  question: string;
  onConfirm: (confirmed: boolean) => void;
}

// ── Helpers ───────────────────────────────────────────────────────

let _msgCounter = 0;
function msgId(): string {
  return `msg-${Date.now()}-${++_msgCounter}`;
}

function getInitials(name: string): string {
  return name
    .split(/[\s-]+/)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase() ?? "")
    .join("");
}

// ── Component ─────────────────────────────────────────────────────

export interface SlackHomeProps {
  /** Router push function for navigating to other views */
  push: (view: { name: string; props?: Record<string, unknown> }) => void;
}

export function SlackHome({ push }: SlackHomeProps): React.JSX.Element {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 120;
  const rows = stdout?.rows ?? 40;

  // ── Services ──
  const chatService = getChatService();
  const agentService = getAgentService();

  // ── Revision counters (trigger re-render on service updates) ──
  const [chatRevision, setChatRevision] = useState(0);
  const [agentRevision, setAgentRevision] = useState(0);

  useEffect(() => {
    const unsubChat = chatService.subscribe(() => setChatRevision((r) => r + 1));
    const unsubAgent = agentService.subscribe(() => setAgentRevision((r) => r + 1));
    return () => { unsubChat(); unsubAgent(); };
  }, []);

  // ── Focus state ──
  const [focusSection, setFocusSection] = useState<FocusSection>("compose");

  // ── Sidebar state ──
  const [activeChannelId, setActiveChannelId] = useState<string>("");
  const [sidebarCursor, setSidebarCursor] = useState(0);
  const [collapsedSections, setCollapsedSections] = useState<string[]>([]);

  // ── Thread state ──
  const [threadOpen, setThreadOpen] = useState(false);
  const [threadParentId, setThreadParentId] = useState<string | null>(null);
  const [threadSourceChannel, setThreadSourceChannel] = useState<string>("");

  // ── Quick switcher ──
  const [quickSwitcherOpen, setQuickSwitcherOpen] = useState(false);

  // ── Loading / inline widgets ──
  const [isLoading, setIsLoading] = useState(false);
  const [loadingHint, setLoadingHint] = useState("");
  const [picker, setPicker] = useState<PickerState | null>(null);
  const [confirm, setConfirm] = useState<ConfirmState | null>(null);

  // ── Local messages (system/slash command output) ──
  const [localMessages, setLocalMessages] = useState<ConversationMessage[]>([
    {
      id: "welcome",
      role: "system",
      content: "Welcome to WUPHF. Type a message or use /help for commands.",
      timestamp: Date.now(),
    },
  ]);

  // ── Derive channels and DMs from services ──
  void chatRevision;
  void agentRevision;

  const channels = chatService.getChannels();
  const agents = agentService.list();

  // Build sidebar sections: DMs first, then channels
  const sidebarSections: SidebarSectionData[] = useMemo(() => {
    // DM items: one per agent
    const dmItems: SidebarItemData[] = agents.map((managed) => {
      // Find or infer a DM channel for this agent
      const dmChannel = channels.find(
        (ch) => ch.name === managed.config.slug || ch.name === `dm-${managed.config.slug}`,
      );
      const channelId = dmChannel?.id ?? `dm-${managed.config.slug}`;
      const isOnline = managed.state.phase !== "done" && managed.state.phase !== "error";

      return {
        id: channelId,
        name: managed.config.name,
        type: "dm" as const,
        online: isOnline,
        unread: dmChannel ? chatService.getMessages(dmChannel.id, 100)
          .filter((m) => m.senderType === "agent").length > 0 ? 0 : 0 : 0,
        hasMention: false,
        muted: false,
        lastActivity: Date.now(),
      };
    });

    // Channel items
    const channelItems: SidebarItemData[] = channels
      .filter((ch) => !ch.name.startsWith("dm-"))
      .map((ch) => ({
        id: ch.id,
        name: ch.name,
        type: "channel" as const,
        visibility: "public" as const,
        unread: ch.unread,
        hasMention: false,
        muted: false,
        lastActivity: Date.now(),
      }));

    return [
      { title: "Direct messages", items: dmItems },
      { title: "Channels", items: channelItems },
    ];
  }, [chatRevision, agentRevision]);

  // Set initial active channel: first DM (founding agent) or first channel
  useEffect(() => {
    if (activeChannelId) return;
    const firstDm = sidebarSections[0]?.items[0];
    const firstChannel = sidebarSections[1]?.items[0];
    const initial = firstDm ?? firstChannel;
    if (initial) {
      setActiveChannelId(initial.id);
    }
  }, [sidebarSections, activeChannelId]);

  // Auto-detect missing API key
  useEffect(() => {
    const apiKey = resolveApiKey();
    if (!apiKey) {
      setLocalMessages((prev) => [
        ...prev,
        {
          id: "no-auth",
          role: "system",
          content: "No API key found. Run /init to set up WUPHF.",
          timestamp: Date.now(),
        },
      ]);
    }
  }, []);

  // ── Derive active channel info ──
  const activeItem = useMemo(() => {
    for (const section of sidebarSections) {
      const found = section.items.find((item) => item.id === activeChannelId);
      if (found) return found;
    }
    return null;
  }, [sidebarSections, activeChannelId]);

  const activeChannelType = activeItem?.type ?? "channel";
  const activeChannelName = activeItem?.name ?? "general";

  // ── Derive messages for active channel ──
  const serviceMessages: ChatMessageInput[] = useMemo(() => {
    if (!activeChannelId) return [];
    return chatService.getMessages(activeChannelId).map((m) => ({
      id: m.id,
      sender: m.sender,
      senderType: m.senderType as "agent" | "human",
      content: m.content,
      timestamp: m.timestamp,
    }));
  }, [activeChannelId, chatRevision]);

  // Convert local messages to ChatMessageInput format
  const localChatMessages: ChatMessageInput[] = useMemo(() => {
    return localMessages.map((m) => ({
      id: m.id,
      sender: m.role === "user" ? "you" : m.role === "system" ? "system" : "wuphf",
      senderType: (m.role === "user" ? "human" : "system") as "agent" | "human" | "system",
      content: m.content,
      timestamp: m.timestamp,
      isError: m.isError,
    }));
  }, [localMessages]);

  // Merge and sort
  const allMessages = useMemo(() => {
    return [...serviceMessages, ...localChatMessages].sort(
      (a, b) => a.timestamp - b.timestamp,
    );
  }, [serviceMessages, localChatMessages]);

  // ── Thread replies ──
  const threadReplies: ThreadMessage[] = useMemo(() => {
    if (!threadParentId || !threadSourceChannel) return [];
    // Get replies to the parent message from the service
    const msgs = chatService.getMessages(threadSourceChannel);
    return msgs
      .filter((m) => m.replyTo === threadParentId)
      .map((m) => ({
        id: m.id,
        sender: m.sender,
        senderType: m.senderType as "agent" | "human" | "system",
        initials: getInitials(m.sender),
        content: m.content,
        timestamp: m.timestamp,
        isFirstInGroup: true, // simplification for now
      }));
  }, [threadParentId, threadSourceChannel, chatRevision]);

  const threadParentMessage: ThreadMessage | null = useMemo(() => {
    if (!threadParentId) return null;
    const msg = allMessages.find((m) => m.id === threadParentId);
    if (!msg) return null;
    return {
      id: msg.id,
      sender: msg.sender,
      senderType: msg.senderType as "agent" | "human" | "system",
      initials: getInitials(msg.sender),
      content: msg.content,
      timestamp: msg.timestamp,
      isFirstInGroup: true,
    };
  }, [threadParentId, allMessages]);

  // ── Quick switcher items ──
  const switcherItems: QuickSwitcherItem[] = useMemo(() => {
    const items: QuickSwitcherItem[] = [];
    for (const section of sidebarSections) {
      for (const item of section.items) {
        items.push({
          id: item.id,
          name: item.name,
          type: item.type,
          online: item.online,
          unread: item.unread,
          score: item.id === activeChannelId ? 200 : item.unread > 0 ? 100 : 50,
        });
      }
    }
    return items;
  }, [sidebarSections, activeChannelId]);

  // ── Slash command entries for autocomplete ──
  const slashCommandEntries = useMemo(
    () =>
      listSlashCommands().map((cmd) => ({
        name: cmd.name,
        description: cmd.description,
        usage: cmd.usage,
      })),
    [],
  );

  // Agent entries for @mention autocomplete
  const agentEntries = useMemo(
    () =>
      agents.map((a) => ({
        slug: a.config.slug,
        name: a.config.name,
      })),
    [agentRevision],
  );

  // ── Slash command context (for init flow and slash commands) ──
  const slashContext: SlashCommandContext = useMemo(() => ({
    push,
    dispatch,
    addMessage: (msg: ConversationMessage) => setLocalMessages((prev) => [...prev, msg]),
    setLoading: (loading: boolean, hint?: string) => {
      setIsLoading(loading);
      setLoadingHint(hint ?? "");
    },
    showPicker: (title: string, options: SelectOption[], onSelect: (value: string) => void) => {
      setPicker({
        title,
        options,
        onSelect: (value: string) => {
          setPicker(null);
          onSelect(value);
        },
      });
    },
    clearPicker: () => setPicker(null),
    showConfirm: (question: string, onConfirm: (confirmed: boolean) => void) => {
      setConfirm({
        question,
        onConfirm: (confirmed: boolean) => {
          setConfirm(null);
          onConfirm(confirmed);
        },
      });
    },
    clearConfirm: () => setConfirm(null),
  }), [push]);

  // ── Message send handler ──
  const handleSend = useCallback(async (input: string) => {
    if (!input.trim()) return;

    const isSlash = input.trimStart().startsWith("/");
    const isInitInput = getInitState().phase !== "idle";

    // Handle init flow
    if (isInitInput) {
      try {
        await handleInitInput(input, slashContext);
      } catch (err) {
        setLocalMessages((prev) => [
          ...prev,
          {
            id: msgId(),
            role: "assistant",
            content: `Init error: ${err instanceof Error ? err.message : String(err)}`,
            timestamp: Date.now(),
            isError: true,
          },
        ]);
      }
      return;
    }

    // Handle agent wizard text input phases
    if (getAgentWizardState().phase !== "idle") {
      try {
        await handleAgentWizardInput(input, slashContext);
      } catch (err) {
        setLocalMessages((prev) => [
          ...prev,
          {
            id: msgId(),
            role: "assistant",
            content: `Error: ${err instanceof Error ? err.message : String(err)}`,
            timestamp: Date.now(),
            isError: true,
          },
        ]);
      }
      return;
    }

    // Handle slash commands
    if (isSlash) {
      const parsed = parseSlashInput(input);
      if (!parsed.command) {
        setLocalMessages((prev) => [
          ...prev,
          {
            id: msgId(),
            role: "assistant",
            content: "Type /help to see available commands.",
            timestamp: Date.now(),
            isError: true,
          },
        ]);
        return;
      }

      if (parsed.command === "clear") {
        setLocalMessages([
          { id: msgId(), role: "system", content: "Conversation cleared.", timestamp: Date.now() },
        ]);
        return;
      }

      const cmd = getSlashCommand(parsed.command);
      if (cmd) {
        setIsLoading(true);
        setLoadingHint(`Running /${parsed.command}...`);
        try {
          const result = await cmd.execute(parsed.args ?? "", slashContext);
          if (result.output && !result.silent) {
            setLocalMessages((prev) => [
              ...prev,
              { id: msgId(), role: "assistant", content: result.output!, timestamp: Date.now() },
            ]);
          }
        } catch (err) {
          setLocalMessages((prev) => [
            ...prev,
            {
              id: msgId(),
              role: "assistant",
              content: `Error: ${err instanceof Error ? err.message : String(err)}`,
              timestamp: Date.now(),
              isError: true,
            },
          ]);
        } finally {
          setIsLoading(false);
        }
      } else {
        setLocalMessages((prev) => [
          ...prev,
          {
            id: msgId(),
            role: "assistant",
            content: `Unknown command: /${parsed.command}. Type /help for available commands.`,
            timestamp: Date.now(),
            isError: true,
          },
        ]);
      }
      return;
    }

    // Natural language message
    if (activeChannelId) {
      chatService.send(activeChannelId, input, "human");

      // Route to agent if this is a DM
      if (activeChannelType === "dm") {
        // Find the agent for this DM
        const agent = agents.find((a) => {
          const dmId = `dm-${a.config.slug}`;
          return activeChannelId === dmId || activeChannelId.includes(a.config.slug);
        });
        if (agent) {
          try {
            agentService.steer(agent.config.slug, input);
          } catch {
            // Agent may not be started — that's OK
          }
        }
      }
    } else {
      // No channel — use ask dispatch as fallback
      setIsLoading(true);
      setLoadingHint("thinking...");
      try {
        const result = await dispatch(`ask ${input}`);
        const isAuthError = result.exitCode === 2 || result.error?.includes("API key");
        setLocalMessages((prev) => [
          ...prev,
          {
            id: msgId(),
            role: "assistant",
            content: isAuthError
              ? "Your API key is missing or expired.\n\nTo fix:\n  1. Get a key at https://app.nex.ai\n  2. Run: wuphf config set api_key <key>\n  3. Then try again."
              : result.error
                ? `Error: ${result.error}`
                : result.output || "(no response)",
            timestamp: Date.now(),
            isError: !!result.error,
          },
        ]);
      } finally {
        setIsLoading(false);
      }
    }
  }, [activeChannelId, activeChannelType, agents, slashContext]);

  // ── Thread handlers ──
  const handleOpenThread = useCallback((messageId: string) => {
    setThreadOpen(true);
    setThreadParentId(messageId);
    setThreadSourceChannel(activeChannelId);
    setFocusSection("thread");
  }, [activeChannelId]);

  const handleCloseThread = useCallback(() => {
    setThreadOpen(false);
    setThreadParentId(null);
    setThreadSourceChannel("");
    setFocusSection("compose");
  }, []);

  const handleThreadReply = useCallback((content: string) => {
    if (!threadParentId || !threadSourceChannel) return;
    chatService.send(threadSourceChannel, content, "human");
  }, [threadParentId, threadSourceChannel]);

  // ── Sidebar handlers ──
  const handleSidebarSelect = useCallback((id: string) => {
    setActiveChannelId(id);
    setFocusSection("compose");
  }, []);

  const handleToggleSection = useCallback((title: string) => {
    setCollapsedSections((prev) =>
      prev.includes(title)
        ? prev.filter((t) => t !== title)
        : [...prev, title],
    );
  }, []);

  // ── Quick switcher handlers ──
  const handleQuickSwitcherSelect = useCallback((id: string) => {
    setActiveChannelId(id);
    setQuickSwitcherOpen(false);
    setFocusSection("compose");
  }, []);

  // ── Expose globalThis callbacks for app.tsx key interception ──
  useEffect(() => {
    (globalThis as Record<string, unknown>).__nexQuickSwitcherOpen = () => {
      setQuickSwitcherOpen(true);
    };
    (globalThis as Record<string, unknown>).__nexQuickSwitcherClose = () => {
      setQuickSwitcherOpen(false);
    };
    (globalThis as Record<string, unknown>).__nexSlackFocusCycle = (direction: number) => {
      setFocusSection((prev) => {
        const sections: FocusSection[] = threadOpen
          ? ["sidebar", "messages", "compose", "thread"]
          : ["sidebar", "messages", "compose"];
        const idx = sections.indexOf(prev);
        const next = (idx + direction + sections.length) % sections.length;
        return sections[next];
      });
    };
    (globalThis as Record<string, unknown>).__nexSidebarNav = (direction: number) => {
      // Count total visible items for upper-bound clamping
      let totalItems = 0;
      for (const section of sidebarSections) {
        if (!collapsedSections.includes(section.title)) {
          totalItems += section.items.length;
        }
      }
      setSidebarCursor((prev) => Math.max(0, Math.min(totalItems - 1, prev + direction)));
    };
    (globalThis as Record<string, unknown>).__nexSidebarSelect = () => {
      // Flatten sidebar to find item at cursor
      let idx = 0;
      for (const section of sidebarSections) {
        if (collapsedSections.includes(section.title)) continue;
        for (const item of section.items) {
          if (idx === sidebarCursor) {
            handleSidebarSelect(item.id);
            return;
          }
          idx++;
        }
      }
    };
    (globalThis as Record<string, unknown>).__nexSidebarToggle = () => {
      // Toggle section at cursor position
      let idx = 0;
      for (const section of sidebarSections) {
        if (idx === sidebarCursor) {
          handleToggleSection(section.title);
          return;
        }
        idx++;
        if (!collapsedSections.includes(section.title)) {
          idx += section.items.length;
        }
      }
    };
    (globalThis as Record<string, unknown>).__nexThreadClose = () => {
      handleCloseThread();
    };
    (globalThis as Record<string, unknown>).__nexAddChannel = () => {
      // Push to a simple /channel-create prompt view or run /channel slash command
      setLocalMessages((prev) => [...prev, {
        id: msgId(), role: "system",
        content: 'Type `/channel create <name>` to create a channel, or Ctrl+K to search.',
        timestamp: Date.now(),
      }]);
      setFocusSection("compose");
    };
    (globalThis as Record<string, unknown>).__nexAddAgent = () => {
      void openAgentsManager(slashContext);
      setFocusSection("compose");
    };
    (globalThis as Record<string, unknown>).__nexFocusSection = focusSection;
    (globalThis as Record<string, unknown>).__nexQuickSwitcherIsOpen = quickSwitcherOpen;

    return () => {
      delete (globalThis as Record<string, unknown>).__nexQuickSwitcherOpen;
      delete (globalThis as Record<string, unknown>).__nexQuickSwitcherClose;
      delete (globalThis as Record<string, unknown>).__nexSlackFocusCycle;
      delete (globalThis as Record<string, unknown>).__nexSidebarNav;
      delete (globalThis as Record<string, unknown>).__nexSidebarSelect;
      delete (globalThis as Record<string, unknown>).__nexSidebarToggle;
      delete (globalThis as Record<string, unknown>).__nexThreadClose;
      delete (globalThis as Record<string, unknown>).__nexAddChannel;
      delete (globalThis as Record<string, unknown>).__nexAddAgent;
      delete (globalThis as Record<string, unknown>).__nexFocusSection;
      delete (globalThis as Record<string, unknown>).__nexQuickSwitcherIsOpen;
    };
  }, [
    focusSection, threadOpen, quickSwitcherOpen, sidebarSections,
    collapsedSections, sidebarCursor, handleSidebarSelect,
    handleToggleSection, handleCloseThread,
  ]);

  // ── Layout metrics ──
  const layout = computeLayout(cols, threadOpen);

  // ── Render ──
  return (
    <SlackLayout
      cols={cols}
      rows={rows}
      threadOpen={threadOpen}
      focusSection={focusSection}
      sidebar={
        <SlackSidebar
          width={layout.sidebarWidth}
          focused={focusSection === "sidebar"}
          workspaceName="WUPHF Workspace"
          sections={sidebarSections}
          collapsedSections={collapsedSections}
          activeChannelId={activeChannelId}
          cursor={sidebarCursor}
          onToggleSection={handleToggleSection}
          onSelectItem={handleSidebarSelect}
        />
      }
      main={
        <>
          {/* Channel header */}
          <ChannelHeader
            name={activeChannelName}
            type={activeChannelType}
            online={activeItem?.online}
            focused={focusSection === "messages"}
          />

          {/* Message list */}
          <SlackMessageList
            messages={allMessages}
            onThreadOpen={handleOpenThread}
            width={layout.mainWidth}
          />

          {/* Loading indicator */}
          {isLoading && <Spinner label={loadingHint || "thinking..."} />}

          {/* Inline widgets */}
          {picker != null ? (
            <InlineSelect
              title={picker.title}
              options={picker.options}
              onSelect={picker.onSelect}
            />
          ) : confirm != null ? (
            <InlineConfirm
              question={confirm.question}
              onConfirm={confirm.onConfirm}
            />
          ) : (
            <ComposeArea
              channelName={activeChannelName}
              channelType={activeChannelType}
              recipientName={activeChannelType === "dm" ? activeChannelName : undefined}
              focused={focusSection === "compose"}
              onSubmit={handleSend}
              slashCommands={slashCommandEntries}
              agents={agentEntries}
            />
          )}
        </>
      }
      thread={
        threadOpen && threadParentMessage ? (
          <ThreadPanel
            width={layout.threadWidth}
            focused={focusSection === "thread"}
            parentMessage={threadParentMessage}
            replies={threadReplies}
            sourceChannelName={activeChannelName}
            sourceChannelType={activeChannelType}
            alsoSendToChannel={false}
            onSendReply={handleThreadReply}
            onToggleAlsoSend={() => {}}
            onClose={handleCloseThread}
            slashCommands={slashCommandEntries}
            agents={agentEntries}
          />
        ) : undefined
      }
      overlay={
        quickSwitcherOpen ? (
          <QuickSwitcher
            open={quickSwitcherOpen}
            items={switcherItems}
            onSelect={handleQuickSwitcherSelect}
            onClose={() => setQuickSwitcherOpen(false)}
          />
        ) : undefined
      }
    />
  );
}

export default SlackHome;
