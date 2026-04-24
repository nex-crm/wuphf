import type { ReactNode } from "react";

/**
 * Shared @-mention pattern. Matches `@slug` where slug is lowercase
 * alphanumeric-plus-hyphens and starts with a letter or digit. Mirrors the
 * broker-side mentionPattern in internal/team/broker.go so the web stops
 * highlighting exactly what the backend stops routing. Keep the two in sync.
 */
const MENTION_RE = /(?:^|[^a-zA-Z0-9_])@([a-z0-9][a-z0-9-]{1,29})\b/g;

export interface MentionToken {
  kind: "text" | "mention";
  value: string;
}

/**
 * Parse human input into a mix of plain strings and mention tokens,
 * recognising only slugs that match a known agent. Unknown `@foo` stays
 * plain text so conversational `@joedoe` references don't light up the
 * wrong slug.
 *
 * Pure and framework-free — trivial to unit-test and reusable anywhere
 * human-entered text needs mention chips.
 */
export function parseMentions(
  content: string,
  knownSlugs: readonly string[],
): MentionToken[] {
  if (!content) return [];
  const known = new Set(knownSlugs.map((s) => s.toLowerCase()));
  const out: MentionToken[] = [];
  let lastIdx = 0;
  // matchAll over a fresh regex avoids lastIndex leaking between invocations.
  const re = new RegExp(MENTION_RE.source, "g");
  for (const m of content.matchAll(re)) {
    const slug = m[1];
    if (m.index === undefined) continue;
    if (!known.has(slug.toLowerCase())) continue;
    // The pattern captures an optional leading boundary char; preserve it
    // in the preceding text rather than swallowing it.
    const atSign = m[0].indexOf("@") + m.index;
    if (atSign > lastIdx) {
      out.push({ kind: "text", value: content.slice(lastIdx, atSign) });
    }
    out.push({ kind: "mention", value: slug });
    lastIdx = atSign + 1 + slug.length;
  }
  if (lastIdx < content.length) {
    out.push({ kind: "text", value: content.slice(lastIdx) });
  }
  return out;
}

export function renderMentionTokens(tokens: MentionToken[]): ReactNode[] {
  return tokens.map((t, i) => {
    if (t.kind === "mention") {
      return (
        <span key={`m-${i}-${t.value}`} className="mention">
          @{t.value}
        </span>
      );
    }
    return t.value;
  });
}

export function renderMentions(
  content: string,
  knownSlugs: readonly string[],
): ReactNode[] {
  return renderMentionTokens(parseMentions(content, knownSlugs));
}
