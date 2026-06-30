// Chats — talk to your AI to build and tune tools. Post-call iteration lives
// here; the call itself is the primary way to start. Mock data; the composer
// appends locally so the surface is clickable.

import { useMemo, useState } from "react";
import { PhoneCall, Plus, Send } from "lucide-react";

import { Eyebrow } from "../components/primitives";
import { CHATS, type ChatMessage } from "../mock/data";

interface ChatsSurfaceProps {
  onStartCall: () => void;
  onBuild: () => void;
}

export function ChatsSurface({ onStartCall, onBuild }: ChatsSurfaceProps) {
  const [activeId, setActiveId] = useState(CHATS[0]?.id ?? "");
  const [draft, setDraft] = useState("");
  const [extra, setExtra] = useState<Record<string, ChatMessage[]>>({});

  const active = useMemo(
    () => CHATS.find((c) => c.id === activeId),
    [activeId],
  );
  const messages = useMemo(
    () => [...(active?.messages ?? []), ...(extra[activeId] ?? [])],
    [active, extra, activeId],
  );

  function send() {
    const text = draft.trim();
    if (!text) return;
    setExtra((prev) => ({
      ...prev,
      [activeId]: [
        ...(prev[activeId] ?? []),
        { id: `local-${Date.now()}`, from: "you", body: text, at: "now" },
      ],
    }));
    setDraft("");
  }

  return (
    <div className="opr-chat-layout">
      <div className="opr-chat-list">
        <button
          type="button"
          className="opr-btn opr-btn-primary opr-build-cta"
          onClick={onBuild}
          style={{ marginBottom: "var(--space-2)" }}
        >
          <Plus size={14} strokeWidth={1.9} aria-hidden={true} />
          Describe a new tool
        </button>
        <button
          type="button"
          className="opr-call-cta opr-call-cta-secondary"
          onClick={onStartCall}
          style={{ marginBottom: "var(--space-3)" }}
        >
          <PhoneCall size={14} strokeWidth={1.9} aria-hidden={true} />
          Demo workflow to Nex
        </button>
        <Eyebrow>Conversations</Eyebrow>
        <div style={{ marginTop: "var(--space-2)" }}>
          {CHATS.map((c) => (
            <button
              key={c.id}
              type="button"
              className={`opr-chat-listitem${
                c.id === activeId ? " is-active" : ""
              }`}
              onClick={() => setActiveId(c.id)}
            >
              <div className="opr-chat-listitem-title">
                {c.title}
                {c.unread ? (
                  <span
                    className="opr-led opr-led-suggested"
                    style={{ width: 6, height: 6 }}
                  />
                ) : null}
              </div>
              <div className="opr-chat-listitem-sub">{c.subtitle}</div>
            </button>
          ))}
        </div>
      </div>

      <div className="opr-chat-main">
        <div className="opr-banner">{active?.title ?? "Conversation"}</div>
        <div className="opr-chat-scroll">
          {messages.map((m) => (
            <div key={m.id} style={{ display: "contents" }}>
              <div
                className={`opr-msg ${
                  m.from === "ai" ? "opr-msg-ai" : "opr-msg-you"
                }`}
              >
                {m.body}
              </div>
              <div
                className="opr-msg-meta"
                style={{
                  alignSelf: m.from === "you" ? "flex-end" : "flex-start",
                }}
              >
                {m.at}
              </div>
            </div>
          ))}
        </div>
        <div className="opr-composer">
          <input
            className="opr-composer-input"
            aria-label="Message your AI"
            placeholder="Ask your AI to change a tool, or describe a new one..."
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") send();
            }}
          />
          <button
            type="button"
            className="opr-btn opr-btn-primary"
            onClick={send}
          >
            <Send size={14} strokeWidth={1.9} aria-hidden={true} />
            Send
          </button>
        </div>
      </div>
    </div>
  );
}
