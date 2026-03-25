/**
 * Persistent channel color assignment.
 *
 * Well-known channels get fixed colors matching the spec.
 * Unknown channels cycle through the palette like agent-colors.ts.
 */

const CHANNEL_PALETTE = ['cyan', 'green', 'yellow', 'magenta', 'blue', 'red'] as const;
export type ChannelColor = typeof CHANNEL_PALETTE[number];

/** Well-known channel → color mapping. */
const WELL_KNOWN: Record<string, ChannelColor> = {
  general: 'cyan',
  leads: 'green',
  seo: 'yellow',
  support: 'magenta',
  marketing: 'blue',
  alerts: 'red',
};

// Module-level state so colours persist across renders.
const colorMap = new Map<string, ChannelColor>();
let nextIdx = 0;

/**
 * Return the colour assigned to a channel name, assigning a new one on first
 * encounter. Well-known names get fixed colors; others cycle the palette.
 */
export function getChannelColor(channelName: string): ChannelColor {
  const existing = colorMap.get(channelName);
  if (existing !== undefined) return existing;

  const wellKnown = WELL_KNOWN[channelName.toLowerCase()];
  if (wellKnown) {
    colorMap.set(channelName, wellKnown);
    return wellKnown;
  }

  const color = CHANNEL_PALETTE[nextIdx % CHANNEL_PALETTE.length];
  colorMap.set(channelName, color);
  nextIdx++;
  return color;
}

/** Reset all assignments (useful in tests). */
export function resetChannelColors(): void {
  colorMap.clear();
  nextIdx = 0;
}
