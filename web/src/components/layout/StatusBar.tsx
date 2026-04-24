import { useQuery } from "@tanstack/react-query";

import { getHealth } from "../../api/client";
import { useOfficeMembers } from "../../hooks/useMembers";
import { isDMChannel, useAppStore } from "../../stores/app";
import { Kbd } from "../ui/Kbd";

interface HealthSnapshot {
  status: string;
  provider?: string;
  provider_model?: string;
  agents?: Record<string, unknown>;
}

/**
 * Bottom status bar mirroring the legacy IIFE: shows the active channel/app,
 * mode (office vs 1:1), agent count, broker connection, and runtime provider.
 */
export function StatusBar() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const currentApp = useAppStore((s) => s.currentApp);
  const channelMeta = useAppStore((s) => s.channelMeta);
  const brokerConnected = useAppStore((s) => s.brokerConnected);
  const setComposerHelpOpen = useAppStore((s) => s.setComposerHelpOpen);
  const { data: members = [] } = useOfficeMembers();
  const dm = !currentApp ? isDMChannel(currentChannel, channelMeta) : null;

  const { data: health } = useQuery<HealthSnapshot>({
    queryKey: ["health"],
    queryFn: () => getHealth() as Promise<HealthSnapshot>,
    refetchInterval: 15_000,
    enabled: brokerConnected,
  });

  const agentCount = members.filter(
    (m) =>
      m.slug && m.slug !== "human" && m.slug !== "you" && m.slug !== "system",
  ).length;

  const channelLabel = currentApp
    ? currentApp
    : dm
      ? `@${dm.agentSlug}`
      : `# ${currentChannel}`;
  const modeLabel = dm ? "1:1" : "office";
  const provider = health?.provider;
  const providerModel = health?.provider_model?.trim();

  return (
    <div className="status-bar">
      <span className="status-bar-item">{channelLabel}</span>
      <span className="status-bar-item">{modeLabel}</span>
      <span className="status-bar-spacer" />
      <button
        type="button"
        className="status-bar-shortcut"
        onClick={() => setComposerHelpOpen(true)}
        title="Keyboard shortcuts"
        aria-label="Open keyboard shortcuts"
      >
        <Kbd size="sm">?</Kbd>
        <span>shortcuts</span>
      </button>
      <span className="status-bar-item">
        {agentCount} agent{agentCount === 1 ? "" : "s"}
      </span>
      {provider && (
        <span
          className="status-bar-item"
          title={
            providerModel
              ? `Runtime: ${provider} · ${providerModel}`
              : `Runtime provider: ${provider}`
          }
        >
          {"⚙ "}
          {provider}
          {providerModel && (
            <>
              <span className="status-bar-sep"> · </span>
              <span className="status-bar-model">{providerModel}</span>
            </>
          )}
        </span>
      )}
      <span
        className={`status-bar-item status-bar-conn${brokerConnected ? "" : " disconnected"}`}
      >
        {brokerConnected ? "connected" : "disconnected"}
      </span>
    </div>
  );
}
