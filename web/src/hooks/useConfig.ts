import { useQuery } from "@tanstack/react-query";

import {
  type ConfigSnapshot,
  getConfig,
  type OfficeMember,
} from "../api/client";
import type { HarnessKind } from "../lib/harness";

const DEFAULT_HARNESS: HarnessKind = "claude-code";

/**
 * Shared config query — keys + staleTime mirror useDefaultHarness so we
 * hit the same cache entry regardless of which hook the caller picked.
 */
function useConfigSnapshot(): ConfigSnapshot | undefined {
  const { data } = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 60_000,
  });
  return data;
}

/**
 * Returns the install-wide default harness kind, used to render the avatar
 * badge for agents that have no explicit provider binding.
 */
export function useDefaultHarness(): HarnessKind {
  const cfg = useConfigSnapshot();
  const raw = cfg?.llm_provider;
  if (raw === "claude-code" || raw === "codex" || raw === "opencode")
    return raw;
  return DEFAULT_HARNESS;
}

/**
 * Resolve the team-lead slug. Prefers the explicitly configured
 * `team_lead_slug` from /config; falls back to the first built-in office
 * member, then to "ceo" as a last-resort default.
 *
 * Mirrors `resolveLeadSlug` in components/messages/Composer.tsx so any
 * surface that needs to address the lead can do so without parsing config
 * + members itself.
 */
export function resolveLeadSlug(
  configured: string | undefined,
  members: ReadonlyArray<{ slug?: string; built_in?: boolean }>,
): string {
  const explicit = (configured ?? "").trim().toLowerCase();
  if (explicit) return explicit;
  const builtin = members.find(
    (m) => m.built_in && m.slug && m.slug !== "human" && m.slug !== "you",
  );
  if (builtin?.slug) return builtin.slug;
  return "ceo";
}

/**
 * Hook variant of `resolveLeadSlug`. Pulls config + member list internally
 * so callers don't need to wire two separate queries themselves.
 */
export function useTeamLeadSlug(
  members: ReadonlyArray<OfficeMember> | undefined,
): string {
  const cfg = useConfigSnapshot();
  return resolveLeadSlug(cfg?.team_lead_slug, members ?? []);
}
