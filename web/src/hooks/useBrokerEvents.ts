import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { initApi, sseURL } from "../api/client";
import {
  type AgentActivitySnapshot,
  directChannelSlug,
  useAppStore,
} from "../stores/app";

const RECONNECT_GRACE_MS = 5000;

function activeBrokerChannel(): string | null {
  // SSE handler runs outside the React tree. Parse window.location.hash
  // directly so we don't depend on the TanStack router being started in
  // unit tests (the router's matches stay at their initial empty state
  // until a RouterProvider mounts). Hash is the source of truth for
  // hash-history navigation anyway, so the runtime read agrees.
  if (typeof window === "undefined") return null;
  const { hash } = window.location;
  const rawPath = hash.startsWith("#/")
    ? hash.slice(2)
    : hash.replace(/^#/, "");
  // TanStack hash-history can append a search-string after the hash
  // path (e.g. `#/channels/general?modal=settings`). Strip it before
  // segment parsing so the channel slug isn't silently smuggled into
  // the next segment as `general?modal=settings`, which would make the
  // active-channel check fail and unread counts climb while the user
  // is staring at the channel.
  const [path] = rawPath.split("?");
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

function parseActivitySnapshot(event: Event): AgentActivitySnapshot | null {
  if (!("data" in event) || typeof event.data !== "string") return null;
  try {
    const parsed = JSON.parse(event.data) as Partial<AgentActivitySnapshot>;
    if (typeof parsed?.slug !== "string" || parsed.slug.length === 0) {
      return null;
    }
    return parsed as AgentActivitySnapshot;
  } catch (err) {
    // Malformed SSE payload — log a breadcrumb (matches `api/pam.ts` pattern
    // for SSE parse failures) and let the cache invalidation still fire.
    console.warn("useBrokerEvents: malformed activity payload", err);
    return null;
  }
}

export function useBrokerEvents(enabled: boolean) {
  const queryClient = useQueryClient();
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected);
  const recordActivitySnapshot = useAppStore((s) => s.recordActivitySnapshot);
  const setIsReconnecting = useAppStore((s) => s.setIsReconnecting);

  useEffect(() => {
    if (!enabled) return;

    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES) return;

    const source = new ES(sseURL("/events"));
    let graceTimer: ReturnType<typeof setTimeout> | null = null;

    function clearGrace(): void {
      if (graceTimer !== null) {
        clearTimeout(graceTimer);
        graceTimer = null;
      }
    }

    function startGrace(): void {
      if (graceTimer !== null) return;
      graceTimer = setTimeout(() => {
        graceTimer = null;
        // Re-check readyState at fire time — EventSource may have
        // auto-reconnected silently inside the 5s window without firing
        // an onopen we noticed. This avoids flagging a transient blip
        // as a sustained disconnect.
        if (source.readyState !== source.OPEN) {
          setIsReconnecting(true);
        }
      }, RECONNECT_GRACE_MS);
    }

    source.addEventListener("ready", () => {
      // Refresh the module-level auth token before marking the broker as
      // connected. In direct mode the broker issues a fresh /web-token on
      // each startup, so subsequent API calls would use a stale bearer if
      // we don't re-handshake here. initApi() is a no-op in proxy mode.
      void initApi().finally(() => setBrokerConnected(true));
    });
    source.addEventListener("open", () => {
      // Native EventSource fires an "open" event when the connection (re)opens.
      // Cancel any pending reconnect grace timer and clear the flag.
      clearGrace();
      setIsReconnecting(false);
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
    source.addEventListener("activity", (event) => {
      // CRITICAL REGRESSION: cache invalidation MUST keep firing — other
      // surfaces (workspace presence, member list) depend on it. The new
      // store write happens AFTER and is wrapped so a snapshot-record throw
      // can never block the invalidation path.
      void queryClient.invalidateQueries({ queryKey: ["office-members"] });
      void queryClient.invalidateQueries({ queryKey: ["channel-members"] });
      try {
        const snapshot = parseActivitySnapshot(event);
        if (snapshot) {
          recordActivitySnapshot(snapshot);
        }
      } catch (err) {
        // Defensive: any throw here would otherwise abort the SSE listener
        // for the rest of the session (EventSource doesn't recover from a
        // synchronous handler throw). Keep going.
        console.warn("useBrokerEvents: activity store write failed", err);
      }
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
    source.onerror = () => {
      setBrokerConnected(false);
      // EventSource auto-reconnects; only mark "reconnecting" once the
      // socket has stayed in a not-OPEN state for the 5s grace window.
      if (source.readyState !== source.OPEN) {
        startGrace();
      }
    };

    return () => {
      clearGrace();
      // Reset the user-visible reconnect flag on unmount so a remount
      // (route change) starts from a clean slate rather than inheriting
      // a stale "Reconnecting…" indicator.
      setIsReconnecting(false);
      source.close();
    };
  }, [
    enabled,
    queryClient,
    setBrokerConnected,
    recordActivitySnapshot,
    setIsReconnecting,
  ]);
}
