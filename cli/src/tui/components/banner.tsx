/**
 * Animated ASCII banner — procedurally generated header matching the Go CLI.
 *
 * Features:
 *  - Flowing line patterns with dots/connectors (─●───○──)
 *  - "wuphf · powered by wuphf.ai" centered
 *  - Regenerates on interval (default: every 3 seconds)
 *  - Colors: cyan ("NexBlue") for "wuphf", muted for rest
 *  - ~6 lines tall, responsive to terminal width
 */

import React, { useState, useEffect, useMemo, useCallback } from "react";
import { Box, Text, useStdout } from "ink";

// ── Pattern atoms ────────────────────────────────────────────────────

const LINE_CHARS = ["─", "─", "─", "─", "─", "─", "●", "○", "◦", "·"];
const CONNECTOR_CHARS = ["┤", "├", "│", "╌"];

/**
 * Build one decorative line of exactly `width` characters.
 * The line is a random mix of horizontal segments, dots, and connectors
 * that gives the flowing "─●───○──" look.
 */
export function generateLine(width: number, seed?: number): string {
  let s = seed ?? Math.floor(Math.random() * 0xffff);
  const next = (): number => {
    s = (s * 1103515245 + 12345) & 0x7fffffff;
    return s;
  };

  const buf: string[] = [];
  let remaining = width;

  while (remaining > 0) {
    const r = next();
    // Mostly horizontal dashes with occasional dots/connectors
    if (remaining > 3 && r % 7 === 0) {
      // Insert a connector surrounded by dashes
      const connector = CONNECTOR_CHARS[r % CONNECTOR_CHARS.length];
      buf.push("─", connector, "─");
      remaining -= 3;
    } else if (r % 5 === 0) {
      // Insert a dot character
      const dot = LINE_CHARS[6 + (r % 4)]; // ●, ○, ◦, ·
      buf.push(dot);
      remaining -= 1;
    } else {
      buf.push("─");
      remaining -= 1;
    }
  }

  return buf.join("").slice(0, width);
}

/**
 * Build a centered brand line within `width` columns.
 * Returns segments for colorized rendering.
 */
export interface BrandSegment {
  text: string;
  type: "brand" | "muted" | "line";
}

export function buildBrandLine(width: number, seed?: number): BrandSegment[] {
  const brandText = "wuphf";
  const tagline = " · powered by wuphf.ai";
  const inner = brandText + tagline;
  const padding = 2; // space each side of inner text
  const innerWidth = inner.length + padding * 2;

  if (width < innerWidth + 4) {
    // Too narrow — just show brand
    const side = Math.max(0, Math.floor((width - brandText.length) / 2));
    const leftLine = generateLine(side, seed);
    const rightLine = generateLine(Math.max(0, width - side - brandText.length), seed ? seed + 1 : undefined);
    return [
      { text: leftLine, type: "line" },
      { text: brandText, type: "brand" },
      { text: rightLine, type: "line" },
    ];
  }

  const sideWidth = Math.floor((width - innerWidth) / 2);
  const rightWidth = width - sideWidth - innerWidth;
  const leftLine = generateLine(sideWidth, seed);
  const rightLine = generateLine(rightWidth, seed ? seed + 1 : undefined);

  return [
    { text: leftLine, type: "line" },
    { text: " ".repeat(padding), type: "muted" },
    { text: brandText, type: "brand" },
    { text: tagline, type: "muted" },
    { text: " ".repeat(padding), type: "muted" },
    { text: rightLine, type: "line" },
  ];
}

/**
 * Generate the full banner frame — an array of line data.
 * Each entry is either "decorative" (a flowing line) or "brand" (the centered text line).
 */
export interface BannerFrame {
  lines: Array<{ segments: BrandSegment[] }>;
}

export function generateBannerFrame(width: number, seed?: number): BannerFrame {
  let s = seed ?? Math.floor(Math.random() * 0xffff);
  const lines: BannerFrame["lines"] = [];

  // Line 1: top decorative
  lines.push({ segments: [{ text: generateLine(width, s), type: "line" }] });
  s += 7;

  // Line 2: decorative with a gap
  lines.push({ segments: [{ text: generateLine(width, s), type: "line" }] });
  s += 13;

  // Line 3: brand line
  lines.push({ segments: buildBrandLine(width, s) });
  s += 17;

  // Line 4: decorative
  lines.push({ segments: [{ text: generateLine(width, s), type: "line" }] });
  s += 11;

  // Line 5: decorative
  lines.push({ segments: [{ text: generateLine(width, s), type: "line" }] });
  s += 3;

  // Line 6: bottom decorative (sparser)
  lines.push({ segments: [{ text: generateLine(width, s + 999), type: "line" }] });

  return { lines };
}

// ── Props ────────────────────────────────────────────────────────────

export interface BannerProps {
  /** Override terminal width (for testing or fixed layouts) */
  width?: number;
  /** Regeneration interval in ms. Default 3000. Set 0 to disable animation. */
  interval?: number;
}

// ── Component ────────────────────────────────────────────────────────

export function Banner({ width: widthProp, interval = 3000 }: BannerProps): React.JSX.Element {
  const { stdout } = useStdout();
  const termWidth = widthProp ?? (stdout?.columns ?? 80);
  const effectiveWidth = Math.min(termWidth, 120); // cap at 120 for readability

  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (interval <= 0) return;
    const id = setInterval(() => setTick((t) => t + 1), interval);
    return () => clearInterval(id);
  }, [interval]);

  const frame = useMemo(
    () => generateBannerFrame(effectiveWidth, tick * 31337),
    [effectiveWidth, tick],
  );

  const renderSegment = useCallback((seg: BrandSegment, idx: number) => {
    switch (seg.type) {
      case "brand":
        return (
          <Text key={idx} color="cyan" bold>
            {seg.text}
          </Text>
        );
      case "muted":
        return (
          <Text key={idx} dimColor>
            {seg.text}
          </Text>
        );
      case "line":
        return (
          <Text key={idx} dimColor>
            {seg.text}
          </Text>
        );
    }
  }, []);

  return (
    <Box flexDirection="column">
      {frame.lines.map((line, i) => (
        <Box key={i}>
          {line.segments.map((seg, j) => renderSegment(seg, j))}
        </Box>
      ))}
    </Box>
  );
}

export default Banner;
