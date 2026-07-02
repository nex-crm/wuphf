// SimpleAgentDetail — the stripped-down agent view: the agent IS its chat.
// Exactly three sections and nothing else. Chat (default) takes the whole main
// screen with the session strip on top; Tools lists what the chat has built and
// can call; Integrations is what the agent can reach. No routines, artifacts,
// data, or knowledge tabs — this variant bets everything on the conversation.

import { useEffect, useState } from "react";
import { ArrowLeft, Trash2 } from "lucide-react";

import { get } from "../../api/client";
import { AgentName } from "../agents/AgentName";
import { AgentSessions } from "../agents/AgentSessions";
import {
  appBuildState,
  useDeleteApp,
  useOperatorApp,
} from "../apps/useOperatorApps";
import { type TabDef, Tabs } from "../components/primitives";
import { ToolsProvider } from "../tools/toolsContext";
import { AppToolsTab } from "./AppToolsTab";
import { ToolIntegrations } from "./ToolIntegrations";

type SimpleSection = "chat" | "tools" | "integrations";

const SECTIONS: readonly TabDef<SimpleSection>[] = [
  { id: "chat", label: "Chat" },
  { id: "tools", label: "Tools" },
  { id: "integrations", label: "Integrations" },
];

interface SimpleAgentDetailProps {
  appId: string;
  onBack: () => void;
}

export function SimpleAgentDetail({ appId, onBack }: SimpleAgentDetailProps) {
  const [section, setSection] = useState<SimpleSection>("chat");
  const query = useOperatorApp(appId);
  const remove = useDeleteApp();

  const app = query.data?.app;
  const state = app ? appBuildState(app) : "building";
  const failed = state === "failed";
  const ready = state === "ready";

  return (
    // Key the provider on the loaded identity: it mounts before the app query
    // resolves, so remount once the real agent arrives instead of keeping
    // tools state seeded from the "This agent" placeholder.
    <ToolsProvider
      key={app?.id ?? "loading"}
      appName={app?.name ?? "This agent"}
      agentId={app?.id}
    >
      <div className="opr-simple-agent">
        <div className="opr-simple-head">
          <button type="button" className="opr-back" onClick={onBack}>
            <ArrowLeft size={13} strokeWidth={1.9} aria-hidden={true} />
            All agents
          </button>
          <span className="opr-tool-emoji" aria-hidden={true}>
            {app?.icon || "🧩"}
          </span>
          <div className="opr-detail-titles">
            <div className="opr-detail-name">
              {app ? (
                <AgentName id={app.id} fallback={app.name} />
              ) : (
                "Loading agent…"
              )}
            </div>
            <div className="opr-tool-meta">
              <span
                className={`opr-pill ${failed ? "opr-pill-bad" : "opr-pill-muted"}`}
              >
                <span
                  className={`opr-led ${
                    failed
                      ? "opr-led-bad"
                      : ready
                        ? "opr-led-live"
                        : "opr-led-draft"
                  }`}
                />
                {failed ? "Failed" : ready ? "Live" : "Building"}
              </span>
              {app ? (
                <span className="opr-meta-dot">v{app.version}</span>
              ) : null}
            </div>
          </div>
          {failed ? (
            <div className="opr-detail-actions">
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() =>
                  app && remove.mutate(app.id, { onSuccess: onBack })
                }
                disabled={remove.isPending}
              >
                <Trash2 size={13} strokeWidth={1.9} aria-hidden={true} />
                Remove
              </button>
            </div>
          ) : null}
        </div>

        <Tabs tabs={SECTIONS} active={section} onSelect={setSection} />

        <div
          className="opr-simple-body"
          role="tabpanel"
          id={`opr-panel-${section}`}
          aria-labelledby={`opr-tab-${section}`}
        >
          {/* The chat stays MOUNTED across section switches — hidden, not
              unmounted — so peeking at Tools never loses an in-flight
              conversation. Tools/Integrations mount only while active. */}
          <div
            className="opr-simple-chat"
            style={section === "chat" ? undefined : { display: "none" }}
          >
            <AgentSessions
              agentName={app?.name ?? "This agent"}
              agentId={app?.id}
            />
          </div>
          {section === "tools" ? (
            <div className="opr-simple-scroll">
              <AppToolsTab appName={app?.name ?? "This agent"} />
            </div>
          ) : null}
          {section === "integrations" ? (
            <div className="opr-simple-scroll">
              <SimpleIntegrations />
            </div>
          ) : null}
        </div>
      </div>
    </ToolsProvider>
  );
}

// The broker's app-scoped integration catalog: the CONNECTED platforms the
// agent can call (mirrors appIntegrationCatalogResponse in
// internal/team/broker_apps_integrations.go). Passing the connected names into
// ToolIntegrations stops the section from falsely claiming "does not use any
// integrations"; a fetch failure degrades to the plain catalog.
interface AppIntegrationCatalogItem {
  platform: string;
  name: string;
  logo_url?: string;
  read_actions: string[];
}

function SimpleIntegrations() {
  const [connectedNames, setConnectedNames] = useState<string[]>([]);
  useEffect(() => {
    let cancelled = false;
    get<{ connected?: AppIntegrationCatalogItem[] }>(
      "/apps/integrations/catalog",
    )
      .then((data) => {
        if (cancelled) return;
        const names = (data.connected ?? [])
          .map((c) => (c.name || c.platform || "").trim())
          .filter((n) => n.length > 0);
        setConnectedNames(names);
      })
      .catch(() => {
        // Broker unreachable — keep the plain catalog.
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return (
    <ToolIntegrations
      usedNames={connectedNames}
      usedLabel="This agent can use"
    />
  );
}
