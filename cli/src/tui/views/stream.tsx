/**
 * Stream view — Claude Code-style single chat stream.
 *
 * Rendering approach (matches Claude Code patterns):
 * - React.memo on message components to skip re-renders
 * - useMemo on visible message slice for stable reference
 * - No overflow="hidden" — compute exact message count to fit naturally
 * - Conditional rendering returns null to remove elements from tree
 * - marginBottom={0} explicit on message containers
 */

import React, { useState, useCallback, useEffect, useRef, useMemo } from "react";
import { Box, Text, useStdout } from "ink";
import { TextInput } from "@inkjs/ui";
import { spawn as nodeSpawn } from "node:child_process";
import { existsSync as fsExists, readFileSync as fsRead, unlinkSync as fsUnlink, mkdirSync as fsMkdir } from "node:fs";
import { join as pathJoin } from "node:path";
import { tmpdir as osTmpdir } from "node:os";
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
import { AgentRoster } from "../components/agent-roster.js";
import {
  useSlashAutocomplete,
  SlashAutocomplete,
} from "../components/slash-autocomplete.js";
import type { SlashCommandEntry } from "../components/slash-autocomplete.js";
import {
  useMentionAutocomplete,
  MentionAutocomplete,
} from "../components/mention-autocomplete.js";
import type { AgentEntry } from "../components/mention-autocomplete.js";
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

// ── Message component (React.memo — skip re-render when props unchanged) ──

const StreamMsg = React.memo(function StreamMsg({ msg }: { msg: StreamMessage }): React.JSX.Element {
  if (msg.senderType === "system") {
    return (
      <Box paddingX={1} marginBottom={0}>
        <Text color="gray">{msg.content}</Text>
      </Box>
    );
  }

  const isHuman = msg.senderType === "human";
  const nameColor = isHuman ? "cyan" : "yellow";
  const prefix = isHuman ? "You" : msg.sender;

  return (
    <Box flexDirection="column" paddingX={1} marginBottom={0}>
      <Box gap={1}>
        <Text color={nameColor} bold>{prefix}</Text>
        {msg.isStreaming ? <Text color="gray">...</Text> : null}
      </Box>
      <Box paddingLeft={2}>
        <Text color={msg.isError ? "red" : undefined} wrap="wrap">
          {msg.content}
        </Text>
      </Box>
    </Box>
  );
});

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
  const [inputValue, setInputValue] = useState("");
  const nextDefaultRef = useRef("");

  // Slash command entries for autocomplete
  const slashCommands: SlashCommandEntry[] = useMemo(() => {
    return listSlashCommands().map(c => ({
      name: c.name,
      description: c.description,
      usage: c.usage,
    }));
  }, []);

  // Slash autocomplete
  const { state: slashState, actions: slashActions } = useSlashAutocomplete(slashCommands);

  // @mention autocomplete — list agents (re-derives when agents change)
  const [agentRevision, setAgentRevision] = useState(0);
  const agentEntries: AgentEntry[] = useMemo(() => {
    void agentRevision; // trigger recompute
    return getAgentService().list().map(a => ({
      slug: a.config.slug,
      name: a.config.name,
    }));
  }, [agentRevision]);
  const { state: mentionState, actions: mentionActions } = useMentionAutocomplete(agentEntries, inputValue);

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

    // Wire agent events to the stream
    const wiredAgents = new Set<string>();
    const wireAgentEvents = () => {
      for (const managed of agentService.list()) {
        if (wiredAgents.has(managed.config.slug)) continue;
        wiredAgents.add(managed.config.slug);

        // Agent text responses → stream messages
        managed.loop.on("message", (content: unknown) => {
          if (typeof content !== "string" || !content.trim()) return;
          setMessages(prev => [...prev, {
            id: msgId(),
            sender: managed.config.name,
            senderType: "agent",
            content,
            timestamp: Date.now(),
          }]);
        });

        // Agent done/error → clear loading
        managed.loop.on("phase_change", (_prev: unknown, next: unknown) => {
          if (next === "done" || next === "error") {
            setIsLoading(false);
          }
        });
      }
    };
    wireAgentEvents();

    // Re-wire when agents are added + bump revision for @mention list
    const unsub = agentService.subscribe(() => {
      wireAgentEvents();
      setAgentRevision(r => r + 1);
    });
    // Bump once now that Team-Lead was created above
    setAgentRevision(r => r + 1);
    return unsub;
  }, []);

  const addMessage = useCallback((msg: Omit<StreamMessage, "id" | "timestamp">) => {
    setMessages(prev => [...prev, { ...msg, id: msgId(), timestamp: Date.now() }]);
  }, []);

  const remountInput = useCallback((text: string) => {
    nextDefaultRef.current = text;
    setInputValue(text);
    setSubmitKey(k => k + 1);
  }, []);

  // TextInput's built-in suggestions for Tab autocomplete
  const inputSuggestions = useMemo(() => {
    if (inputValue.startsWith("/")) {
      return slashCommands.map(c => `/${c.name}`);
    }
    if (inputValue.includes("@")) {
      // Extract text before and after the last @
      const atIdx = inputValue.lastIndexOf("@");
      const prefix = inputValue.slice(0, atIdx);
      return agentEntries.map(a => `${prefix}@${a.slug} `);
    }
    return [];
  }, [inputValue, slashCommands, agentEntries]);

  const handleInputChange = useCallback((value: string) => {
    setInputValue(value);
    // Slash autocomplete
    if (value.startsWith("/")) {
      slashActions.update(value);
    } else if (slashState.visible) {
      slashActions.onDismiss();
    }
    // @mention autocomplete
    if (value.includes("@") && agentEntries.length > 0) {
      mentionActions.update(value);
    } else if (mentionState.visible) {
      mentionActions.onDismiss();
    }
  }, [slashActions, slashState.visible, mentionActions, mentionState.visible, agentEntries.length]);

  // Expose Tab/arrow handlers via globalThis so app.tsx can intercept them.
  // Use refs to always read fresh state without re-creating the callbacks.
  const slashStateRef = useRef(slashState);
  slashStateRef.current = slashState;
  const mentionStateRef = useRef(mentionState);
  mentionStateRef.current = mentionState;

  useEffect(() => {
    const g = globalThis as Record<string, unknown>;

    g.__nexHomeTabComplete = (direction: number): boolean => {
      if (slashStateRef.current.visible) {
        const result = direction < 0 ? slashActions.onShiftTab() : slashActions.onTab();
        if (result) remountInput(result.text);
        return true;
      }
      if (mentionStateRef.current.visible) {
        const result = direction < 0 ? mentionActions.onShiftTab() : mentionActions.onTab();
        if (result) remountInput(result.text);
        return true;
      }
      return false;
    };

    g.__nexHomeAutocompleteNav = (direction: number): boolean => {
      if (slashStateRef.current.visible) { slashActions.onNavigate(direction); return true; }
      if (mentionStateRef.current.visible) { mentionActions.onNavigate(direction); return true; }
      return false;
    };

    return () => {
      delete g.__nexHomeTabComplete;
      delete g.__nexHomeAutocompleteNav;
    };
  }, [slashActions, mentionActions, remountInput]);

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
    // If autocomplete is visible, accept the selection instead of submitting
    if (slashState.visible) {
      const result = slashActions.onAccept();
      if (result) { remountInput(result.text); return; }
    }
    if (mentionState.visible) {
      const result = mentionActions.onAccept();
      if (result) { remountInput(result.text); return; }
    }

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

    // Natural language → route through agent system
    addMessage({ sender: "you", senderType: "human", content: trimmed });

    const agentService = getAgentService();
    const teamLead = agentService.get("team-lead");

    if (teamLead) {
      setIsLoading(true);
      setLoadingHint("thinking...");

      // Spawn claude -p as a detached process that writes to a temp file.
      // Poll for the done marker with setInterval (sync existsSync).
      // This is the only pattern that works in Bun+Ink: detached process +
      // sync file polling from setInterval (no async, no Promises, no Workers).
      const outDir = pathJoin(osTmpdir(), "wuphf-claude-out");
      fsMkdir(outDir, { recursive: true });
      const outFile = pathJoin(outDir, `msg-${Date.now()}.json`);
      const doneFile = `${outFile}.done`;

      // Strip Claude nesting env vars (Paperclip pattern) — prevents the child
      // claude from thinking it's nested inside another Claude Code session.
      const cleanEnv = { ...process.env };
      delete cleanEnv.CLAUDECODE;
      delete cleanEnv.CLAUDE_CODE_ENTRYPOINT;
      delete cleanEnv.CLAUDE_CODE_SESSION;
      delete cleanEnv.CLAUDE_CODE_PARENT_SESSION;

      // Escape single quotes in prompt for safe shell embedding
      const safePrompt = trimmed.replace(/'/g, "'\\''");
      const shellCmd = `claude -p '${safePrompt}' --output-format stream-json --verbose --max-turns 5 --no-session-persistence --allowedTools 'Read,Glob,Grep,WebSearch,WebFetch' > '${outFile}' 2>/dev/null; touch '${doneFile}'`;
      const proc = nodeSpawn("sh", ["-c", shellCmd], {
        stdio: "ignore",
        detached: true,
        env: cleanEnv,
      });
      proc.unref();

      // Poll with setInterval (sync check, works in Bun+Ink)
      const pollId = setInterval(() => {
        if (!fsExists(doneFile)) return; // not done yet
        clearInterval(pollId);
        setIsLoading(false);

        let stdout = "";
        try { stdout = fsRead(outFile, "utf-8"); } catch { /* no output */ }
        try { fsUnlink(outFile); } catch {}
        try { fsUnlink(doneFile); } catch {}

        // Parse NDJSON for assistant text
        let responseText = "";
        for (const line of stdout.split("\n")) {
          try {
            const event = JSON.parse(line);
            if (event.type === "assistant" && event.message?.content) {
              for (const part of (event.message.content as Array<Record<string, unknown>>)) {
                if (part.type === "text" && part.text) responseText += part.text as string;
              }
            }
            if (event.type === "result" && !responseText && event.result) {
              responseText = event.result as string;
            }
          } catch { continue; }
        }

        addMessage({
          sender: "Team-Lead",
          senderType: "agent",
          content: responseText || "(no response from Claude Code)",
        });
      }, 500);
    } else {
      // Fallback: no Team-Lead agent, use WUPHF Ask API directly
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
    }
  }, [addMessage, remountInput, slashContext, slashState.visible, slashActions, mentionState.visible, mentionActions]);

  // Compute exact message count to fit terminal (no overflow clipping needed)
  // Reserve: 3 lines for input box (border + text + border), 1 for spinner, 1 for padding
  const maxVisible = Math.max(rows - 5, 6);
  const displayMessages = useMemo(
    () => messages.slice(-maxVisible),
    [messages, maxVisible],
  );

  return (
    <Box flexDirection="column">
      {/* Message stream + agent roster side by side */}
      <Box flexDirection="row">
        <Box flexDirection="column" flexGrow={1}>
          {displayMessages.map(msg => (
            <StreamMsg key={msg.id} msg={msg} />
          ))}

          {/* Loading indicator — removed from tree entirely when not loading */}
          {isLoading ? (
            <Box paddingX={1} marginBottom={0}>
              <Spinner label={loadingHint || "thinking..."} />
            </Box>
          ) : null}
        </Box>

        {/* Agent roster — Discord-style right panel */}
        <AgentRoster />
      </Box>

      {/* Autocomplete overlays */}
      <SlashAutocomplete state={slashState} />
      <MentionAutocomplete state={mentionState} />

      {/* Picker / Confirm / Input */}
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
            suggestions={inputSuggestions}
            onChange={handleInputChange}
            onSubmit={handleSubmit}
          />
        </Box>
      )}
    </Box>
  );
}

export default StreamHome;
