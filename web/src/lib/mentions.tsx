import type { ReactNode } from "react";

/**
 * Shared @-mention pattern. Matches `@slug` where slug is lowercase
 * alphanumeric-plus-hyphens and starts with a letter or digit. Mirrors the
 * broker-side mentionPattern in internal/team/broker.go so the web stops
 * highlighting exactly what the backend stops routing. Keep the two in sync.
 */
const MENTION_RE = /(?:^|[^a-zA-Z0-9_])@([a-z0-9][a-z0-9-]{1,29})\b/g;
const SPECIAL_MENTION_SLUGS = ["all"] as const;
const NON_MENTIONABLE_SLUGS = new Set(["human", "you", "system"]);

export interface MentionToken {
  kind: "text" | "mention";
  value: string;
}

function normalizeMentionableSlugs(knownSlugs: readonly string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of knownSlugs) {
    const slug = raw.trim().toLowerCase();
    if (!slug || NON_MENTIONABLE_SLUGS.has(slug) || seen.has(slug)) continue;
    seen.add(slug);
    out.push(slug);
  }
  return out;
}

function knownMentionSet(knownSlugs: readonly string[]): Set<string> {
  return new Set([
    ...normalizeMentionableSlugs(knownSlugs),
    ...SPECIAL_MENTION_SLUGS,
  ]);
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
  const known = knownMentionSet(knownSlugs);
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

/**
 * Extract the agent slugs that should be sent in the broker's `tagged`
 * payload. `@all` expands to every known agent slug because the HTTP
 * message endpoint validates tagged members individually.
 */
export function extractTaggedMentions(
  content: string,
  knownSlugs: readonly string[],
): string[] {
  if (!content) return [];
  const available = normalizeMentionableSlugs(knownSlugs);
  const known = knownMentionSet(knownSlugs);
  const tagged: string[] = [];
  const seen = new Set<string>();
  let wantsAll = false;
  const re = new RegExp(MENTION_RE.source, "g");
  for (const match of content.matchAll(re)) {
    const slug = match[1]?.toLowerCase();
    if (!slug || !known.has(slug)) continue;
    if (slug === "all") {
      wantsAll = true;
      continue;
    }
    if (seen.has(slug)) continue;
    seen.add(slug);
    tagged.push(slug);
  }
  if (wantsAll) return available;
  return tagged;
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
