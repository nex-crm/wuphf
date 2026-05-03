import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChatBubble,
  CheckCircle,
  NavArrowRight,
  Terminal,
} from "iconoir-react";

import {
  fetchCommands,
  getRequests,
  type Message,
  type SlashCommandDescriptor,
} from "../../api/client";
import { getOfficeTasks } from "../../api/tasks";
import { FALLBACK_SLASH_COMMANDS } from "../../hooks/useCommands";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useMessages } from "../../hooks/useMessages";
import { useAppStore } from "../../stores/app";

interface CommandRow {
  name: string;
  description: string;
  webSupported: boolean;
  appTarget?: string;
}

interface TerminalLine {
  id: string;
  time: string;
  speaker: string;
  content: string;
}

const COMMAND_APP_TARGETS: Record<string, string> = {
  calendar: "calendar",
  policies: "policies",
  requests: "requests",
  skills: "skills",
  tasks: "tasks",
  threads: "threads",
};

function normalizeCommandName(command: string): string {
  return command.replace(/^\/+/, "").trim().toLowerCase();
}

function commandRowsFromRegistry(
  commands: SlashCommandDescriptor[] | undefined,
): CommandRow[] {
  if (!commands || commands.length === 0) {
    return FALLBACK_SLASH_COMMANDS.map((command) => {
      const name = normalizeCommandName(command.name);
      return {
        name: command.name,
        description: command.desc,
        webSupported: true,
        appTarget: COMMAND_APP_TARGETS[name],
      };
    });
  }
  return commands.map((command) => {
    const name = normalizeCommandName(command.name);
    return {
      name: `/${name}`,
      description: command.description,
      webSupported: command.webSupported,
      appTarget: COMMAND_APP_TARGETS[name],
    };
  });
}

function terminalTime(timestamp: string | undefined): string {
  if (!timestamp) return "--:--";
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) return "--:--";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function terminalLineFromMessage(message: Message): TerminalLine {
  const content = (message.content || "").replace(/\s+/g, " ").trim();
  return {
    id:
      message.id ||
      `${message.timestamp || "no-time"}-${message.from || "agent"}-${content}`,
    time: terminalTime(message.timestamp),
    speaker: message.from === "you" ? "you" : `@${message.from || "agent"}`,
    content: content || "(empty)",
  };
}

function activeTaskCount(tasks: Array<{ status?: string }>): number {
  return tasks.filter((task) => {
    const status = (task.status || "open").toLowerCase();
    return status !== "done" && status !== "canceled" && status !== "cancelled";
  }).length;
}

function openRequestCount(requests: Array<{ status?: string }>): number {
  return requests.filter((request) => {
    const status = (request.status || "open").toLowerCase();
    return status === "open" || status === "pending";
  }).length;
}

export function TuiApp() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const messages = useMessages(currentChannel);
  const members = useOfficeMembers();
  const tasks = useQuery({
    queryKey: ["tui-office-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: 10_000,
  });
  const requests = useQuery({
    queryKey: ["tui-requests", currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
  });
  const commandRegistry = useQuery({
    queryKey: ["tui-command-registry"],
    queryFn: fetchCommands,
    staleTime: 5 * 60_000,
    retry: 1,
  });

  const terminalLines = useMemo(
    () => (messages.data ?? []).slice(-18).map(terminalLineFromMessage),
    [messages.data],
  );
  const commandRows = useMemo(
    () => commandRowsFromRegistry(commandRegistry.data),
    [commandRegistry.data],
  );
  const activeMembers = (members.data ?? []).filter(
    (member) =>
      member.slug !== "human" &&
      member.slug !== "you" &&
      (member.status || member.task || member.activity),
  );
  const taskCount = activeTaskCount(tasks.data?.tasks ?? []);
  const requestCount = openRequestCount(requests.data?.requests ?? []);
  const supportedCount = commandRows.filter((row) => row.webSupported).length;

  return (
    <div className="tui-app" data-testid="tui-app">
      <header className="tui-header">
        <div className="tui-title">
          <Terminal className="tui-title-icon" />
          <h2>TUI</h2>
          <span className="badge badge-neutral">web</span>
        </div>
        <div className="tui-header-meta">
          <span>#{currentChannel || "general"}</span>
          <span>{activeMembers.length} active</span>
          <span>{supportedCount} commands</span>
        </div>
      </header>

      <div className="tui-grid">
        <section className="tui-terminal" aria-label="TUI transcript mirror">
          <div className="tui-terminal-bar">
            <span>wuphf office</span>
            <span>{terminalLines.length} lines</span>
          </div>
          <div className="tui-terminal-body">
            {terminalLines.length > 0 ? (
              terminalLines.map((line) => (
                <div className="tui-line" key={line.id}>
                  <span className="tui-line-time">{line.time}</span>
                  <span className="tui-line-speaker">{line.speaker}</span>
                  <span className="tui-line-content">{line.content}</span>
                </div>
              ))
            ) : (
              <div className="tui-line tui-line-muted">
                <span className="tui-line-time">--:--</span>
                <span className="tui-line-speaker">system</span>
                <span className="tui-line-content">#{currentChannel}</span>
              </div>
            )}
            <div className="tui-prompt">
              <span>wuphf:{currentChannel || "general"}$</span>
              <span className="tui-cursor" aria-hidden="true" />
            </div>
          </div>
        </section>

        <aside className="tui-stack">
          <section className="tui-stat-row" aria-label="TUI queue">
            <button
              type="button"
              className="tui-stat"
              onClick={() => setCurrentApp("tasks")}
            >
              <CheckCircle />
              <span>
                <strong>{taskCount}</strong>
                <small>tasks</small>
              </span>
            </button>
            <button
              type="button"
              className="tui-stat"
              onClick={() => setCurrentApp("requests")}
            >
              <ChatBubble />
              <span>
                <strong>{requestCount}</strong>
                <small>requests</small>
              </span>
            </button>
          </section>

          <section className="tui-panel" aria-label="TUI app jump targets">
            <div className="tui-panel-title">Apps</div>
            {["tasks", "requests", "threads", "skills", "calendar"].map(
              (app) => (
                <button
                  type="button"
                  className="tui-jump"
                  key={app}
                  onClick={() => setCurrentApp(app)}
                >
                  <span>{app}</span>
                  <NavArrowRight />
                </button>
              ),
            )}
          </section>

          <section className="tui-panel" aria-label="TUI slash commands">
            <div className="tui-panel-title">Slash</div>
            <div className="tui-command-list">
              {commandRows.slice(0, 12).map((command) => {
                const content = (
                  <>
                    <span className="tui-command-name">{command.name}</span>
                    <span className="tui-command-desc">
                      {command.description}
                    </span>
                  </>
                );
                if (command.appTarget) {
                  return (
                    <button
                      type="button"
                      className="tui-command"
                      key={command.name}
                      onClick={() => setCurrentApp(command.appTarget ?? null)}
                    >
                      {content}
                    </button>
                  );
                }
                return (
                  <div
                    className={`tui-command${command.webSupported ? "" : " tui-command-muted"}`}
                    key={command.name}
                  >
                    {content}
                  </div>
                );
              })}
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}

export const __test__ = {
  activeTaskCount,
  commandRowsFromRegistry,
  openRequestCount,
  terminalLineFromMessage,
};
