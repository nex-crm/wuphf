import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { ensureWorkflowChannel } from "../../api/workflows";
import { Composer } from "../messages/Composer";
import { MessageFeed } from "../messages/MessageFeed";

/**
 * WorkflowBuilderChat — the collapsible "chat with the builder" pane on a frozen
 * workflow card. The operator talks to @workflow-builder about THIS
 * contract: ask it to add a step, change a trigger, accept a healing overlay,
 * or run it. The Workflow Builder is the only agent with the workflow_* tools, so its changes
 * land on the spec and the card's graph + run history refetch.
 *
 * It reuses the SAME shared chat primitives as the channel and task surfaces —
 * `MessageFeed` (live stream + typing loader + threads + reactions) and
 * `Composer` (`@mentions`, `/slash`, history recall) — bound to the per-workflow
 * channel `workflow-<spec_id>`. The channel is ensured lazily on first open so
 * existing frozen workflows (minted before this feature) get one on demand.
 */
export default function WorkflowBuilderChat({ specId }: { specId: string }) {
  const [open, setOpen] = useState(false);

  // Ensure the channel only once the pane is actually opened — no need to mint
  // a channel for every frozen workflow the operator never chats about.
  const channel = useQuery({
    queryKey: ["workflows", "channel", specId],
    queryFn: () => ensureWorkflowChannel(specId),
    enabled: open,
    staleTime: Number.POSITIVE_INFINITY,
  });

  const slug = channel.data?.channel;

  return (
    <div
      style={{
        borderTop: "1px solid var(--border)",
        paddingTop: 10,
        display: "flex",
        flexDirection: "column",
        gap: 8,
      }}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          width: "100%",
          background: "transparent",
          border: "none",
          cursor: "pointer",
          padding: 0,
          color: "var(--text-secondary)",
          textAlign: "left",
        }}
      >
        <span
          style={{
            transform: open ? "rotate(90deg)" : "none",
            transition: "transform .12s ease",
            fontSize: 11,
          }}
        >
          ▶
        </span>
        <span
          style={{
            fontSize: 11.5,
            fontWeight: 700,
            letterSpacing: ".06em",
            textTransform: "uppercase",
          }}
        >
          Ask the builder
        </span>
        <span style={{ fontSize: 12, fontWeight: 400 }}>
          — chat with @workflow-builder to change this workflow
        </span>
      </button>

      {open && (
        <div
          style={{
            border: "1px solid var(--border)",
            borderRadius: 10,
            background: "var(--bg-card)",
            overflow: "hidden",
            display: "flex",
            flexDirection: "column",
            maxHeight: 420,
          }}
        >
          {channel.isLoading && (
            <div
              style={{
                padding: 14,
                fontSize: 12.5,
                color: "var(--text-secondary)",
              }}
            >
              Opening the workflow channel…
            </div>
          )}
          {channel.error ? (
            <div style={{ padding: 14, fontSize: 12.5, color: "var(--red)" }}>
              Couldn't open the chat:{" "}
              {channel.error instanceof Error
                ? channel.error.message
                : "unknown error"}
            </div>
          ) : null}
          {slug ? (
            <section
              className="conversation-chat"
              aria-label="Workflow builder conversation"
              data-testid="workflow-builder-chat"
              style={{ display: "flex", flexDirection: "column", minHeight: 0 }}
            >
              <MessageFeed channel={slug} />
              <Composer channel={slug} />
            </section>
          ) : null}
        </div>
      )}
    </div>
  );
}
