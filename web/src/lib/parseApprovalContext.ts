// Parses the structured approval context produced by buildActionApprovalSpec
// (Go: internal/teammcp/actions.go). The Go side renders a plain string
// because the broker stores `context` as a single string field; this parser
// recovers the structure so the overlay can lay out Why / Details / Footer
// with appropriate visual hierarchy. Falls back to null when the input is a
// legacy or non-approval context blob — the caller renders that as plain
// pre-wrap text.

export interface ApprovalDetail {
  label: string;
  value: string;
  truncated: boolean;
}

export interface ApprovalContext {
  why: string | null;
  details: ApprovalDetail[];
  footer: {
    action: string | null;
    account: string | null;
    channel: string | null;
  };
}

const TRUNCATION_MARKER = "…";

const WHY_PATTERN =
  /^Why:\s+([^\n]+(?:\n(?!What this will do:|Action:|Account:|Channel:)[^\n]+)*)/m;
const DETAILS_BLOCK_PATTERN =
  /^What this will do:\s*\n([\s\S]+?)(?=\n\nAction:|\nAction:)/m;
const ACTION_PATTERN = /^Action:\s+(.+)$/m;
const ACCOUNT_PATTERN = /^Account:\s+(.+)$/m;
const CHANNEL_PATTERN = /^Channel:\s+(.+)$/m;
const FOOTER_GUARD = /^Action:\s+/m;

export function parseApprovalContext(
  raw: string | undefined | null,
): ApprovalContext | null {
  const text = (raw ?? "").trim();
  if (!(text && FOOTER_GUARD.test(text))) return null;

  const footer = parseFooter(text);
  if (!(footer.action || footer.channel)) return null;

  return {
    why: parseWhy(text),
    details: parseDetails(text),
    footer,
  };
}

function parseWhy(text: string): string | null {
  const match = text.match(WHY_PATTERN);
  return match ? match[1].trim() : null;
}

function parseDetails(text: string): ApprovalDetail[] {
  const block = text.match(DETAILS_BLOCK_PATTERN);
  if (!block) return [];
  const out: ApprovalDetail[] = [];
  for (const line of block[1].split("\n")) {
    const detail = parseDetailLine(line);
    if (detail) out.push(detail);
  }
  return out;
}

function parseDetailLine(line: string): ApprovalDetail | null {
  const trimmed = line.trim();
  if (!trimmed.startsWith("• ")) return null;
  const stripped = trimmed.slice(2).trim();
  const colonIdx = stripped.indexOf(":");
  if (colonIdx === -1) return null;
  const label = stripped.slice(0, colonIdx).trim();
  if (!label) return null;
  const value = stripped.slice(colonIdx + 1).trim();
  return {
    label,
    value,
    truncated: value.endsWith(TRUNCATION_MARKER),
  };
}

function parseFooter(text: string): ApprovalContext["footer"] {
  return {
    action: matchGroup(text, ACTION_PATTERN),
    account: matchGroup(text, ACCOUNT_PATTERN),
    channel: matchGroup(text, CHANNEL_PATTERN),
  };
}

function matchGroup(text: string, pattern: RegExp): string | null {
  const match = text.match(pattern);
  return match ? match[1].trim() : null;
}

export function approvalContextIsEmpty(parsed: ApprovalContext): boolean {
  return !parsed.why && parsed.details.length === 0;
}
