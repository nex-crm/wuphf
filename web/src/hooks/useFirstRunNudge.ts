import { useOfficeMembersMeta } from "./useMembers";

interface FirstRunNudgeResult {
  showNudge: boolean;
}

/**
 * Drives the first-run "→ tag @<agent>" nudge under the FIRST agent row.
 * Source of truth lives on the broker (`/office-members.meta.humanHasPosted`,
 * eng decision A5) — no localStorage, no client-side persistence. The nudge
 * survives reinstall, browser change, and cache clear.
 *
 * Defensive default: only render the nudge when `humanHasPosted` is
 * explicitly `false`. If `meta` is absent (Lane A not yet shipped) or the
 * field is `undefined` (initial paint before the first poll resolves), the
 * nudge stays hidden. This trades "fresh-install nudge on a Lane-A-less
 * backend" for "no flash on every legitimate session" — the latter is the
 * regression the user explicitly called out.
 */
export function useFirstRunNudge(): FirstRunNudgeResult {
  const { data: meta } = useOfficeMembersMeta();
  return { showNudge: meta?.humanHasPosted === false };
}
