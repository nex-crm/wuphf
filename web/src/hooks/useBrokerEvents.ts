import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { initApi, sseURL } from "../api/client";
import { directChannelSlug, useAppStore } from "../stores/app";

function activeBrokerChannel(): string | null {
  // SSE handler runs outside the React tree. Parse window.location.hash
  // directly so we don't depend on the TanStack router being started in
  // unit tests (the router's matches stay at their initial empty state
  // until a RouterProvider mounts). Hash is the source of truth for
  // hash-history navigation anyway, so the runtime read agrees.
  if (typeof window === "undefined") return null;
  const { hash } = window.location;
  const path = hash.startsWith("#/") ? hash.slice(2) : hash.replace(/^#/, "");
  const segs = path.split("/").filter(Boolean);
  if (segs[0] === "channels" && segs[1]) {
    return decodeURIComponent(segs[1]);
  }
  if (segs[0] === "dm" && segs[1]) {
    return directChannelSlug(decodeURIComponent(segs[1]));
  }
  return null;
}

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
    source.addEventListener("ready", () => {
      // Refresh the module-level auth token before marking the broker as
      // connected. In direct mode the broker issues a fresh /web-token on
      // each startup, so subsequent API calls would use a stale bearer if
      // we don't re-handshake here. initApi() is a no-op in proxy mode.
      void initApi().finally(() => setBrokerConnected(true));
    });
    source.addEventListener("message", (event) => {
      const channel = messageChannelFromEvent(event);
      if (channel) {
        const state = useAppStore.getState();
        if (activeBrokerChannel() === channel) {
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
    source.onerror = () => setBrokerConnected(false);

    return () => {
      source.close();
    };
  }, [enabled, queryClient, setBrokerConnected]);
}
