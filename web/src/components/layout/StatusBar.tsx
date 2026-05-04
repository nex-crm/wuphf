import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { restartBroker } from "../../api/client";
import { getHealth, type HealthResponse } from "../../api/platform";
import { useOfficeMembers } from "../../hooks/useMembers";
import { appTitle } from "../../lib/constants";
import { isDMChannel, useAppStore } from "../../stores/app";
import { Kbd } from "../ui/Kbd";
import { StatusPill } from "../workspaces/StatusPill";

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

  const [retrying, setRetrying] = useState(false);

  const handleRestart = useCallback(async () => {
    setRetrying(true);
    try {
      await restartBroker();
      // Broker accepted the restart — it will exit and respawn.
      // The SSE EventSource reconnects automatically; clear the
      // disconnected flag optimistically so the label reflects the
      // in-progress restart rather than the stale disconnected state.
    } catch {
      // Broker unreachable: nothing to do, SSE will reconnect on its own.
    } finally {
      setRetrying(false);
    }
  }, []);
  const { data: members = [] } = useOfficeMembers();
  const dm = !currentApp ? isDMChannel(currentChannel, channelMeta) : null;

  const { data: health } = useQuery<HealthResponse>({
    queryKey: ["health"],
    queryFn: () => getHealth(),
    refetchInterval: 15_000,
    enabled: brokerConnected,
  });

  const agentCount = members.filter(
    (m) =>
      m.slug && m.slug !== "human" && m.slug !== "you" && m.slug !== "system",
  ).length;

  const channelLabel = currentApp
    ? appTitle(currentApp)
    : dm
      ? `@${dm.agentSlug}`
      : `# ${currentChannel}`;
  const modeLabel = dm ? "1:1" : "office";
  const provider = health?.provider;
  const providerModel = health?.provider_model?.trim();

  return (
    <div className="status-bar">
      <StatusPill />
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
      {provider ? (
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
          {providerModel ? (
            <>
              <span className="status-bar-sep"> · </span>
              <span className="status-bar-model">{providerModel}</span>
            </>
          ) : null}
        </span>
      ) : null}
      {brokerConnected ? (
        <span className="status-bar-item status-bar-conn">connected</span>
      ) : (
        <button
          type="button"
          className="status-bar-item status-bar-conn status-bar-conn-retry disconnected"
          onClick={handleRestart}
          disabled={retrying}
          title="Click to restart broker"
        >
          {retrying ? "restarting…" : "disconnected"}
        </button>
      )}
    </div>
  );
}
