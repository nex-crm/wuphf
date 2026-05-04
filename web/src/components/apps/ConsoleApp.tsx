import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChatBubble, CheckCircle, Terminal } from "iconoir-react";

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
}

interface TerminalLine {
  id: string;
  time: string;
  speaker: string;
  content: string;
}

function normalizeCommandName(command: string): string {
  return command.replace(/^\/+/, "").trim().toLowerCase();
}

function commandRowsFromRegistry(
  commands: SlashCommandDescriptor[] | undefined,
): CommandRow[] {
  if (!commands || commands.length === 0) {
    return FALLBACK_SLASH_COMMANDS.map((command) => ({
      name: command.name,
      description: command.desc,
      webSupported: true,
    }));
  }
  return commands.map((command) => {
    const name = normalizeCommandName(command.name);
    return {
      name: `/${name}`,
      description: command.description,
      webSupported: command.webSupported,
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

export function ConsoleApp() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const [draft, setDraft] = useState("");
  const [localLines, setLocalLines] = useState<TerminalLine[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const terminalBodyRef = useRef<HTMLDivElement>(null);
  const previousChannelRef = useRef(currentChannel);
  const messages = useMessages(currentChannel);
  const members = useOfficeMembers();
  const tasks = useQuery({
    queryKey: ["console-office-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: 10_000,
  });
  const requests = useQuery({
    queryKey: ["console-requests", currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
  });
  const commandRegistry = useQuery({
    queryKey: ["console-command-registry"],
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
  const visibleTerminalLines = useMemo(
    () => [...terminalLines, ...localLines].slice(-18),
    [terminalLines, localLines],
  );
  const activeMembers = (members.data ?? []).filter(
    (member) =>
      member.slug !== "human" &&
      member.slug !== "you" &&
      (member.status || member.task || member.activity),
  );
  const taskCount = activeTaskCount(tasks.data?.tasks ?? []);
  const requestCount = openRequestCount(requests.data?.requests ?? []);
  const commandCount = commandRows.length;

  useEffect(() => {
    if (previousChannelRef.current === currentChannel) return;
    previousChannelRef.current = currentChannel;
    setLocalLines([]);
  }, [currentChannel]);

  useEffect(() => {
    const el = terminalBodyRef.current;
    if (!el || visibleTerminalLines.length === 0) return;
    el.scrollTop = el.scrollHeight;
  }, [visibleTerminalLines]);

  function focusInput(selectionEnd?: number) {
    window.requestAnimationFrame(() => {
      const input = inputRef.current;
      if (!input) return;
      input.focus();
      const end = selectionEnd ?? input.value.length;
      input.setSelectionRange(end, end);
    });
  }

  function insertCommand(commandName: string) {
    const next = draft.trim()
      ? `${draft.trimEnd()} ${commandName} `
      : `${commandName} `;
    setDraft(next);
    focusInput(next.length);
  }

  function submitDraft(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const content = draft.trim();
    if (!content) {
      focusInput();
      return;
    }
    const now = new Date();
    setLocalLines((lines) =>
      [
        ...lines,
        {
          id: `local-${now.getTime()}-${content}`,
          time: terminalTime(now.toISOString()),
          speaker: "you",
          content,
        },
      ].slice(-8),
    );
    setDraft("");
    focusInput();
  }

  return (
    <div className="console-app" data-testid="console-app">
      <header className="console-header">
        <div className="console-title">
          <Terminal className="console-title-icon" />
          <h2>Console</h2>
          <span className="badge badge-neutral">web</span>
        </div>
        <div className="console-header-meta">
          <span>#{currentChannel || "general"}</span>
          <span>{activeMembers.length} active</span>
          <span>{commandCount} commands</span>
        </div>
      </header>

      <div className="console-grid">
        <section
          className="console-terminal"
          aria-label="Console transcript mirror"
        >
          <div className="console-terminal-bar">
            <span>wuphf office</span>
            <span>{visibleTerminalLines.length} lines</span>
          </div>
          <div className="console-terminal-body" ref={terminalBodyRef}>
            {visibleTerminalLines.length > 0 ? (
              visibleTerminalLines.map((line) => (
                <div className="console-line" key={line.id}>
                  <span className="console-line-time">{line.time}</span>
                  <span className="console-line-speaker">{line.speaker}</span>
                  <span className="console-line-content">{line.content}</span>
                </div>
              ))
            ) : (
              <div className="console-line console-line-muted">
                <span className="console-line-time">--:--</span>
                <span className="console-line-speaker">system</span>
                <span className="console-line-content">
                  #{currentChannel || "general"}
                </span>
              </div>
            )}
            <form className="console-prompt" onSubmit={submitDraft}>
              <span>wuphf:{currentChannel || "general"}$</span>
              <input
                ref={inputRef}
                data-testid="console-input"
                aria-label="Console input"
                autoCapitalize="off"
                autoComplete="off"
                autoCorrect="off"
                spellCheck={false}
                value={draft}
                onChange={(event) => setDraft(event.currentTarget.value)}
              />
            </form>
          </div>
        </section>

        <aside className="console-stack">
          <section className="console-stat-row" aria-label="Console queue">
            <button
              type="button"
              className="console-stat"
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
              className="console-stat"
              onClick={() => setCurrentApp("requests")}
            >
              <ChatBubble />
              <span>
                <strong>{requestCount}</strong>
                <small>requests</small>
              </span>
            </button>
          </section>

          <section
            className="console-panel"
            aria-label="Console slash commands"
          >
            <div className="console-panel-title">Slash</div>
            <div className="console-command-list">
              {commandRows.map((command) => (
                <button
                  type="button"
                  className="console-command"
                  data-command={command.name}
                  key={command.name}
                  onClick={() => insertCommand(command.name)}
                >
                  <span className="console-command-name">{command.name}</span>
                  <span className="console-command-desc">
                    {command.description}
                  </span>
                </button>
              ))}
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
