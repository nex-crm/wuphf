/**
 * Persistent channel color assignment.
 *
 * Each channel name gets a stable color from a palette of terminal-safe
 * colours. Well-known channels get fixed colors; others cycle through
 * the palette.
 *
 * Pattern mirrors agent-colors.ts but for channels.
 */

const CHANNEL_PALETTE = ["cyan", "green", "yellow", "magenta", "blue", "red"] as const;
export type ChannelColor = (typeof CHANNEL_PALETTE)[number];

/** Well-known channel → color mapping matching the task spec. */
const WELL_KNOWN: Record<string, ChannelColor> = {
  general: "cyan",
  leads: "green",
  seo: "yellow",
  support: "magenta",
  research: "blue",
  alerts: "red",
};

// Module-level state so colours persist across renders.
const colorMap = new Map<string, ChannelColor>();
let nextColorIdx = 0;

/**
 * Return the colour assigned to `channelName`, assigning a new one on
 * first encounter. The same name always returns the same colour.
 */
export function getChannelColor(channelName: string): ChannelColor {
  const normalized = channelName.toLowerCase().replace(/^#/, "");

  const existing = colorMap.get(normalized);
  if (existing !== undefined) return existing;

  // Check well-known mapping first
  const wellKnown = WELL_KNOWN[normalized];
  if (wellKnown) {
    colorMap.set(normalized, wellKnown);
    return wellKnown;
  }

  // Cycle through palette
  const color = CHANNEL_PALETTE[nextColorIdx % CHANNEL_PALETTE.length];
  colorMap.set(normalized, color);
  nextColorIdx++;
  return color;
}

/** Read-only snapshot of the current colour map. */
export function getAllChannelColors(): ReadonlyMap<string, ChannelColor> {
  return new Map(colorMap);
}

/** Reset all assignments (useful in tests). */
export function resetChannelColors(): void {
  colorMap.clear();
  nextColorIdx = 0;
}
