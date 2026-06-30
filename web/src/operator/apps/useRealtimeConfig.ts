// useRealtimeConfig — reads broker config to decide whether the real voice call
// is available (an OpenAI Realtime key is set) and which model is configured.
// With no key, OperatorApp falls back to the scripted mock CallModal.

import { useQuery } from "@tanstack/react-query";

import { get } from "../../api/client";

interface ConfigStatus {
  openai_key_set?: boolean;
  realtime_model?: string;
}

export interface RealtimeConfig {
  available: boolean;
  model: string;
}

export function useRealtimeConfig(): RealtimeConfig {
  const q = useQuery({
    queryKey: ["operator-config"],
    queryFn: () => get<ConfigStatus>("/config"),
    staleTime: 30_000,
  });
  return {
    available: Boolean(q.data?.openai_key_set),
    model: q.data?.realtime_model ?? "gpt-realtime",
  };
}
