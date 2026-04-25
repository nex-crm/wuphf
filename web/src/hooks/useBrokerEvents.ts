import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { sseURL } from "../api/client";
import { useAppStore } from "../stores/app";

export function useBrokerEvents(enabled: boolean) {
  const queryClient = useQueryClient();
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected);

  useEffect(() => {
    if (!enabled) return;

    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES) return;

    const source = new ES(sseURL("/events"));
    source.addEventListener("ready", () => setBrokerConnected(true));
    source.addEventListener("message", () => {
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
