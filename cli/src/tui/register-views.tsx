/**
 * Registers all view components with the router's view registry.
 * Import this module once (from app.tsx) to wire views into the router.
 *
 * The home view is a Slack-style 3-panel layout (sidebar + main + thread).
 * Slash commands and the init flow remain fully functional.
 */

import React from "react";
import { registerView, useRouter } from "./router.js";
import { useTuiState } from "./tui-context.js";
import { SlackHome } from "./views/slack-home.js";
import { StreamHome } from "./views/stream.js";
import { HelpView } from "./views/help.js";
import { RecordListView } from "./views/record-list.js";
import { RecordDetailView } from "./views/record-detail.js";
import { AskChatView } from "./views/ask-chat.js";
import { AgentListView } from "./views/agent-list.js";
import { ChatView } from "./views/chat.js";
import { CalendarView } from "./views/calendar.js";
import { OrchestrationView } from "./views/orchestration.js";
import { GenerativeView } from "./views/generative.js";
import { dispatch } from "../commands/dispatch.js";
import type { A2UIComponent, A2UIDataModel } from "./generative/types.js";
import { getChatService } from "./services/chat-service.js";
import { getCalendarService } from "./services/calendar-service.js";
import { getAgentService } from "./services/agent-service.js";
import { getOrchestrationService } from "./services/orchestration-service.js";

// ── Home view — Claude Code-style single stream ──

registerView("home", ({ props: _props }) => {
  const { push } = useRouter();
  return <StreamHome push={push} />;
});

// ── Legacy Slack view (accessible via /slack) ──

registerView("slack-home", ({ props: _props }) => {
  const { push } = useRouter();
  return <SlackHome push={push} />;
});

// ── Other views (unchanged) ──

registerView("help", () => <HelpView />);

registerView("record-list", ({ props }) => (
  <RecordListView
    objectType={(props?.objectType as string) ?? "records"}
    records={
      (props?.records as Array<{
        id: string;
        label: string;
        attributes: Record<string, string>;
      }>) ?? []
    }
    columns={(props?.columns as string[]) ?? []}
  />
));

registerView("record-detail", ({ props }) => (
  <RecordDetailView
    objectType={(props?.objectType as string) ?? ""}
    recordId={(props?.recordId as string) ?? ""}
    recordLabel={(props?.recordLabel as string) ?? ""}
    attributes={(props?.attributes as Record<string, string>) ?? {}}
  />
));

registerView("ask-chat", ({ props }) => {
  const tui = useTuiState();
  const mode =
    tui?.state.mode ?? (props?.mode as "normal" | "insert") ?? "insert";

  const handleAsk = async (question: string): Promise<string> => {
    const result = await dispatch(`ask ${question}`);
    if (result.error) return `Error: ${result.error}`;
    return result.output || "(no response)";
  };

  return (
    <AskChatView
      sessionId={props?.sessionId as string | undefined}
      mode={mode}
      onAsk={handleAsk}
    />
  );
});

registerView("agent-list", ({ props }) => {
  const service = getAgentService();

  const mapAgents = () =>
    service.list().map((managed) => ({
      name: managed.config.name,
      status:
        managed.state.phase === "idle"
          ? ("idle" as const)
          : managed.state.phase === "error"
            ? ("error" as const)
            : managed.state.phase === "done"
              ? ("stopped" as const)
              : ("running" as const),
      expertise: managed.config.expertise?.join(", "),
      lastHeartbeat: managed.state.lastHeartbeat,
      nextHeartbeat: managed.state.nextHeartbeat,
    }));

  const [agents, setAgents] = React.useState(mapAgents);

  React.useEffect(() => {
    const update = () => setAgents(mapAgents());
    return service.subscribe(update);
  }, []);

  return (
    <AgentListView
      agents={
        (props?.agents as Array<{
          name: string;
          status: "idle" | "running" | "error" | "stopped";
          expertise?: string;
          lastHeartbeat?: number;
          nextHeartbeat?: number;
        }>) ?? agents
      }
    />
  );
});

registerView("chat", ({ props }) => {
  const tui = useTuiState();
  const mode =
    tui?.state.mode ?? (props?.mode as "normal" | "insert") ?? "normal";
  const service = getChatService();

  const [revision, setRevision] = React.useState(0);

  React.useEffect(() => {
    const update = () => setRevision((r) => r + 1);
    return service.subscribe(update);
  }, []);

  // Re-derive from service on every revision bump
  void revision;
  const channels = service.getChannels();
  const activeChannel = (props?.activeChannel as string) ?? channels[0]?.id;
  const messages = activeChannel ? service.getMessages(activeChannel) : [];

  return (
    <ChatView
      channels={channels}
      messages={messages.map((m) => ({
        id: m.id,
        sender: m.sender,
        content: m.content,
        timestamp: m.timestamp,
        channel: m.channelId,
      }))}
      activeChannel={activeChannel}
      mode={mode}
      onSend={(content, channel) => service.send(channel, content, "human")}
    />
  );
});

registerView("calendar", ({ props }) => {
  const service = getCalendarService();
  const weekOffset = (props?.weekOffset as number) ?? 0;

  const [revision, setRevision] = React.useState(0);

  React.useEffect(() => {
    const update = () => setRevision((r) => r + 1);
    return service.subscribe(update);
  }, []);

  // Re-derive from service on every revision bump
  void revision;
  const events = service.getWeekEvents(weekOffset);

  return <CalendarView events={events} weekOffset={weekOffset} />;
});

registerView("orchestration", ({ props }) => {
  const service = getOrchestrationService();

  const [revision, setRevision] = React.useState(0);

  React.useEffect(() => {
    const update = () => setRevision((r) => r + 1);
    return service.subscribe(update);
  }, []);

  // Re-derive from service on every revision bump
  void revision;
  return (
    <OrchestrationView
      goals={service.getGoals()}
      tasks={service.getTasks()}
      budgets={service.getBudgetSnapshots()}
      globalBudget={service.getGlobalBudget()}
    />
  );
});

registerView("generative", ({ props }) => (
  <GenerativeView
    schema={props?.schema as A2UIComponent}
    data={(props?.data as A2UIDataModel) ?? {}}
    title={props?.title as string | undefined}
  />
));
