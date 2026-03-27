/**
 * Stream view — Claude Code-style single chat stream.
 *
 * One conversation area. User talks, Team-Lead responds and orchestrates
 * specialists behind the scenes. Agent chatter appears live in the stream.
 * No sidebar, no channels, no threads — just clean back-and-forth.
 */

import React, { useState, useCallback, useEffect, useRef } from "react";
import { Box, Text, useStdout } from "ink";
import { TextInput } from "@inkjs/ui";
import { dispatch } from "../../commands/dispatch.js";
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
import { InlineSelect } from "../components/inline-select.js";
import { InlineConfirm } from "../components/inline-confirm.js";
import { Spinner } from "../components/spinner.js";
import { resolveApiKey } from "../../lib/config.js";

// ── Types ─────────────────────────────────────────────────────────

interface StreamMessage {
  id: string;
  sender: string;
  senderType: "human" | "agent" | "system";
  content: string;
  timestamp: number;
  isStreaming?: boolean;
  isError?: boolean;
}

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

let _counter = 0;
function msgId(): string {
  return `s-${Date.now()}-${++_counter}`;
}

// ── Message component ─────────────────────────────────────────────

function StreamMsg({ msg }: { msg: StreamMessage }): React.JSX.Element {
  if (msg.senderType === "system") {
    return (
      <Box paddingX={1} marginY={0}>
        <Text color="gray">{msg.content}</Text>
      </Box>
    );
  }

  const isHuman = msg.senderType === "human";
  const nameColor = isHuman ? "cyan" : "yellow";
  const prefix = isHuman ? "You" : msg.sender;

  return (
    <Box flexDirection="column" paddingX={1} marginY={0}>
      <Box gap={1}>
        <Text color={nameColor} bold>{prefix}</Text>
        {msg.isStreaming && <Text color="gray">...</Text>}
      </Box>
      <Box paddingLeft={2}>
        <Text color={msg.isError ? "red" : undefined} wrap="wrap">
          {msg.content}
        </Text>
      </Box>
    </Box>
  );
}

// ── StreamHome ────────────────────────────────────────────────────

export interface StreamHomeProps {
  push: (view: { name: string; props?: Record<string, unknown> }) => void;
}

export function StreamHome({ push }: StreamHomeProps): React.JSX.Element {
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;

  const [messages, setMessages] = useState<StreamMessage[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [loadingHint, setLoadingHint] = useState("");
  const [submitKey, setSubmitKey] = useState(0);
  const [picker, setPicker] = useState<PickerState | null>(null);
  const [confirm, setConfirm] = useState<ConfirmState | null>(null);
  const nextDefaultRef = useRef("");

  // Auto-create Team-Lead on mount
  useEffect(() => {
    const agentService = getAgentService();
    if (!agentService.get("team-lead")) {
      try {
        agentService.createFromTemplate("team-lead", "team-lead");
      } catch {
        // Template may not exist yet or agent already exists
      }
    }

    // Welcome message
    const apiKey = resolveApiKey();
    setMessages([{
      id: "welcome",
      sender: "system",
      senderType: "system",
      content: apiKey
        ? "What would you like to do?"
        : "Welcome to WUPHF. Type /init to get started.",
      timestamp: Date.now(),
    }]);

    // Subscribe to agent service for live chatter
    const unsub = agentService.subscribe(() => {
      // Agent state changes can trigger UI updates
    });
    return unsub;
  }, []);

  // Wire agent message events to the stream
  useEffect(() => {
    const agentService = getAgentService();
    const agents = agentService.list();
    // This is a simplification — in production, we'd wire each agent's
    // message events. For now, the dispatch("ask ...") path handles responses.
    void agents;
  }, []);

  const addMessage = useCallback((msg: Omit<StreamMessage, "id" | "timestamp">) => {
    setMessages(prev => [...prev, { ...msg, id: msgId(), timestamp: Date.now() }]);
  }, []);

  const remountInput = useCallback((text: string) => {
    nextDefaultRef.current = text;
    setSubmitKey(k => k + 1);
  }, []);

  // Slash command context (for /init, /agents, etc.)
  const slashContext: SlashCommandContext = React.useMemo(() => ({
    push,
    dispatch,
    addMessage: (msg: ConversationMessage) => {
      addMessage({
        sender: msg.role === "user" ? "you" : msg.role === "system" ? "system" : "Team-Lead",
        senderType: msg.role === "user" ? "human" : msg.role === "system" ? "system" : "agent",
        content: msg.content,
        isError: msg.isError,
      });
    },
    setLoading: (loading: boolean, hint?: string) => {
      setIsLoading(loading);
      setLoadingHint(hint ?? "");
    },
    showPicker: (title, options, onSelect) => {
      setPicker({ title, options, onSelect: (v) => { setPicker(null); onSelect(v); } });
    },
    clearPicker: () => setPicker(null),
    showConfirm: (question, onConfirm) => {
      setConfirm({ question, onConfirm: (c) => { setConfirm(null); onConfirm(c); } });
    },
    clearConfirm: () => setConfirm(null),
  }), [push, addMessage]);

  // Main submit handler
  const handleSubmit = useCallback(async (input: string) => {
    const trimmed = input.trim();
    if (!trimmed) return;
    remountInput("");

    // Init flow intercept
    if (getInitState().phase !== "idle") {
      try { await handleInitInput(trimmed, slashContext); } catch (err) {
        addMessage({ sender: "system", senderType: "system", content: `Error: ${err instanceof Error ? err.message : String(err)}`, isError: true });
      }
      return;
    }

    // Agent wizard intercept
    if (getAgentWizardState().phase !== "idle") {
      try { await handleAgentWizardInput(trimmed, slashContext); } catch (err) {
        addMessage({ sender: "system", senderType: "system", content: `Error: ${err instanceof Error ? err.message : String(err)}`, isError: true });
      }
      return;
    }

    // Slash commands
    if (trimmed.startsWith("/")) {
      const parsed = parseSlashInput(trimmed);
      if (parsed.command === "clear") {
        setMessages([{ id: msgId(), sender: "system", senderType: "system", content: "Cleared.", timestamp: Date.now() }]);
        return;
      }
      const cmd = parsed.command ? getSlashCommand(parsed.command) : undefined;
      if (cmd) {
        setIsLoading(true);
        setLoadingHint(`/${parsed.command}...`);
        try {
          const result = await cmd.execute(parsed.args ?? "", slashContext);
          if (result.output && !result.silent) {
            addMessage({ sender: "Team-Lead", senderType: "agent", content: result.output });
          }
        } catch (err) {
          addMessage({ sender: "system", senderType: "system", content: `Error: ${err instanceof Error ? err.message : String(err)}`, isError: true });
        } finally {
          setIsLoading(false);
        }
        return;
      }
      addMessage({ sender: "system", senderType: "system", content: `Unknown command: /${parsed.command}. Type /help for commands.`, isError: true });
      return;
    }

    // Natural language → Team-Lead (via WUPHF Ask API)
    addMessage({ sender: "you", senderType: "human", content: trimmed });
    setIsLoading(true);
    setLoadingHint("thinking...");

    try {
      const result = await dispatch(`ask ${trimmed}`);
      const isAuthError = result.exitCode === 2 || result.error?.includes("API key");

      if (isAuthError) {
        addMessage({
          sender: "system",
          senderType: "system",
          content: "No API key or key expired. Run /init to set up.",
          isError: true,
        });
      } else if (result.error) {
        addMessage({ sender: "system", senderType: "system", content: `Error: ${result.error}`, isError: true });
      } else {
        addMessage({
          sender: "Team-Lead",
          senderType: "agent",
          content: result.output || "(no response)",
        });
      }
    } catch (err) {
      addMessage({
        sender: "system",
        senderType: "system",
        content: `Error: ${err instanceof Error ? err.message : String(err)}`,
        isError: true,
      });
    } finally {
      setIsLoading(false);
    }
  }, [addMessage, remountInput, slashContext]);

  // Visible messages (scroll to show most recent, limited by terminal height)
  const maxVisible = Math.max(rows - 6, 5); // leave room for input + status
  const visibleMessages = messages.slice(-maxVisible);

  return (
    <Box flexDirection="column" height={rows - 1}>
      {/* Message stream */}
      <Box flexDirection="column" flexGrow={1} overflow="hidden">
        {visibleMessages.map(msg => (
          <StreamMsg key={msg.id} msg={msg} />
        ))}

        {/* Loading indicator */}
        {isLoading && (
          <Box paddingX={1}>
            <Spinner label={loadingHint || "thinking..."} />
          </Box>
        )}
      </Box>

      {/* Picker / Confirm / Input */}
      <Box flexDirection="column">
        {picker != null ? (
          <InlineSelect title={picker.title} options={picker.options} onSelect={picker.onSelect} />
        ) : confirm != null ? (
          <InlineConfirm question={confirm.question} onConfirm={confirm.onConfirm} />
        ) : (
          <Box borderStyle="single" borderColor="cyan" paddingX={1}>
            <Text color="cyan" bold>{">"} </Text>
            <TextInput
              key={submitKey}
              defaultValue={nextDefaultRef.current}
              placeholder="Message Team-Lead..."
              onSubmit={handleSubmit}
            />
          </Box>
        )}
      </Box>
    </Box>
  );
}

export default StreamHome;
