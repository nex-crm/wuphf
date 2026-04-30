import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { sseURL } from "../api/client";
import { useAppStore } from "../stores/app";

function messageChannelFromEvent(event: Event): string | null {
  if (!("data" in event) || typeof event.data !== "string") return null;
  try {
    const payload = JSON.parse(event.data) as {
      message?: { channel?: unknown };
    };
    if (typeof payload.message?.channel !== "string") return null;
    const channel = payload.message.channel.trim();
    return channel.length > 0 ? channel : null;
  } catch {
    return null;
  }
}

export function useBrokerEvents(enabled: boolean) {
  const queryClient = useQueryClient();
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected);

  useEffect(() => {
    if (!enabled) return;

    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES) return;

    const source = new ES(sseURL("/events"));
    source.addEventListener("ready", () => setBrokerConnected(true));
    source.addEventListener("message", (event) => {
      const channel = messageChannelFromEvent(event);
      if (channel) {
        const state = useAppStore.getState();
        if (state.currentApp === null && state.currentChannel === channel) {
          state.clearUnread(channel);
        } else {
          state.incrementUnread(channel);
        }
      }
      void queryClient.invalidateQueries({ queryKey: ["messages"] });
      void queryClient.invalidateQueries({ queryKey: ["thread-messages"] });
      void queryClient.invalidateQueries({ queryKey: ["office-members"] });
      void queryClient.invalidateQueries({ queryKey: ["channel-members"] });
    });
    source.addEventListener("activity", () => {
      void queryClient.invalidateQueries({ queryKey: ["office-members"] });
      void queryClient.invalidateQueries({ queryKey: ["channel-members"] });
    });
    source.addEventListener("office_changed", () => {
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
      void queryClient.invalidateQueries({ queryKey: ["office-members"] });
      void queryClient.invalidateQueries({ queryKey: ["channel-members"] });
    });
    source.addEventListener("action", () => {
      void queryClient.invalidateQueries({ queryKey: ["actions"] });
      void queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
    });
    for (const eventName of [
      "surface:created",
      "surface:updated",
      "surface:deleted",
      "surface:widget_created",
      "surface:widget_updated",
      "surface:render_checked",
    ]) {
      source.addEventListener(eventName, () => {
        void queryClient.invalidateQueries({ queryKey: ["surfaces"] });
        void queryClient.invalidateQueries({ queryKey: ["surface-detail"] });
      });
    }
    source.onerror = () => setBrokerConnected(false);

    return () => {
      source.close();
    };
  }, [enabled, queryClient, setBrokerConnected]);
}
