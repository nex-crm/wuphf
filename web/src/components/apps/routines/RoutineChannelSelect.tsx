import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { type Channel, getChannels } from "../../../api/client";

interface RoutineChannelSelectProps {
  value: string;
  onChange: (next: string) => void;
  ownerSlug: string;
  /** test id passthrough so callers can target the underlying select. */
  testId?: string;
}

/**
 * Dropdown for picking where a routine posts when it fires.
 *
 *   - Default value `""` → Owner DM (the backend ensures a `__human` DM
 *     for the owning agent and posts there).
 *   - Other values are team-channel slugs the owner is a member of.
 *
 * DM channels and channels the owner isn't in are filtered out — there's
 * no point listing a channel the agent can't actually read from.
 */
export function RoutineChannelSelect({
  value,
  onChange,
  ownerSlug,
  testId,
}: RoutineChannelSelectProps) {
  const query = useQuery({
    queryKey: ["channels-for-routine"],
    queryFn: () => getChannels(),
  });

  const options = useMemo<Channel[]>(
    () => filterEligibleChannels(query.data?.channels ?? [], ownerSlug),
    [query.data, ownerSlug],
  );

  const dmLabel = ownerSlug
    ? `Owner DM (with @${ownerSlug})`
    : "Owner DM (default)";

  return (
    <select
      className="input"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      data-testid={testId ?? "routine-channel-select"}
      disabled={query.isLoading}
    >
      <option value="">{dmLabel}</option>
      {options.map((ch) => (
        <option key={ch.slug} value={ch.slug}>
          #{ch.slug}
          {ch.name && ch.name !== ch.slug ? ` — ${ch.name}` : ""}
        </option>
      ))}
    </select>
  );
}

/**
 * Filter the channel list down to the ones the owning agent can
 * actually participate in. Rules:
 *
 *   1. Direct-message channels (`type: "dm"`) are excluded; the explicit
 *      "Owner DM" option in the dropdown is the canonical way to opt
 *      into a DM, and surfacing other people's DMs would leak context.
 *   2. If `members[]` is present and non-empty, the owner must be in
 *      it. If `members[]` is absent or empty we treat the channel as
 *      open and include it (most office channels don't enumerate
 *      members on the wire).
 */
export function filterEligibleChannels(
  channels: Channel[],
  ownerSlug: string,
): Channel[] {
  const slug = ownerSlug.trim();
  return channels
    .filter((ch) => (ch.type ?? "").toLowerCase() !== "dm")
    .filter((ch) => {
      if (!ch.members || ch.members.length === 0) return true;
      if (!slug) return true;
      return ch.members.includes(slug);
    })
    .sort((a, b) => a.slug.localeCompare(b.slug));
}
