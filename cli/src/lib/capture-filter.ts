import { stripNexContext } from "./context-format.js";

const MIN_LENGTH = 20;
const MAX_LENGTH = 50_000;

export interface CaptureResult {
  text: string;
  skipped: false;
}

export interface CaptureSkip {
  reason: string;
  skipped: true;
}

export function captureFilter(text: string): CaptureResult | CaptureSkip {
  if (!text || text.trim().length === 0)
    return { skipped: true, reason: "empty text" };
  const cleaned = stripNexContext(text);
  if (cleaned.length < MIN_LENGTH)
    return { skipped: true, reason: `too short (${cleaned.length} chars)` };
  if (cleaned.length > MAX_LENGTH)
    return { skipped: true, reason: `too long (${cleaned.length} chars)` };
  return { skipped: false, text: cleaned };
}
