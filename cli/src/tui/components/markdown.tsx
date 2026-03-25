import React from "react";
import { Box, Text } from "ink";

// --- Types ---

interface MarkdownNode {
  type:
    | "heading"
    | "paragraph"
    | "code_block"
    | "bullet_list"
    | "ordered_list"
    | "blockquote"
    | "blank";
  level?: number; // heading level 1-6, or list indent
  content?: string;
  lines?: string[];
  lang?: string;
}

export interface MarkdownProps {
  content: string;
}

// --- Colors (from bubbletea-ux-spec.md) ---

const COLORS = {
  brand: "#2980fb",
  purple: "#cf72d9",
  info: "#4d97ff",
  muted: "#838485",
  label: "#999a9b",
  value: "#cfd0d2",
  success: "#03a04c",
  warning: "#df750c",
  error: "#e23428",
} as const;

// --- Parser ---

function parseMarkdown(raw: string): MarkdownNode[] {
  const lines = raw.split("\n");
  const nodes: MarkdownNode[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i]!;

    // Fenced code block
    if (/^```/.test(line)) {
      const lang = line.slice(3).trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i]!)) {
        codeLines.push(lines[i]!);
        i++;
      }
      i++; // skip closing ```
      nodes.push({ type: "code_block", lines: codeLines, lang: lang || undefined });
      continue;
    }

    // Blank line
    if (line.trim() === "") {
      nodes.push({ type: "blank" });
      i++;
      continue;
    }

    // Heading
    const headingMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
      nodes.push({
        type: "heading",
        level: headingMatch[1]!.length,
        content: headingMatch[2]!,
      });
      i++;
      continue;
    }

    // Blockquote
    if (/^>\s?/.test(line)) {
      const quoteLines: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i]!)) {
        quoteLines.push(lines[i]!.replace(/^>\s?/, ""));
        i++;
      }
      nodes.push({ type: "blockquote", lines: quoteLines });
      continue;
    }

    // Bullet list
    if (/^[-*+]\s/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*+]\s/.test(lines[i]!)) {
        items.push(lines[i]!.replace(/^[-*+]\s/, ""));
        i++;
      }
      nodes.push({ type: "bullet_list", lines: items });
      continue;
    }

    // Ordered list
    if (/^\d+\.\s/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\d+\.\s/.test(lines[i]!)) {
        items.push(lines[i]!.replace(/^\d+\.\s/, ""));
        i++;
      }
      nodes.push({ type: "ordered_list", lines: items });
      continue;
    }

    // Paragraph (collect consecutive non-special lines)
    const paraLines: string[] = [];
    while (
      i < lines.length &&
      lines[i]!.trim() !== "" &&
      !/^(#{1,6}\s|```|>\s?|[-*+]\s|\d+\.\s)/.test(lines[i]!)
    ) {
      paraLines.push(lines[i]!);
      i++;
    }
    if (paraLines.length > 0) {
      nodes.push({ type: "paragraph", content: paraLines.join(" ") });
    }
  }

  return nodes;
}

// --- Inline renderer ---

interface InlineSegment {
  text: string;
  bold?: boolean;
  italic?: boolean;
  code?: boolean;
}

function parseInline(text: string): InlineSegment[] {
  const segments: InlineSegment[] = [];
  // Match **bold**, *italic*, `code`
  const re = /(\*\*(.+?)\*\*|\*(.+?)\*|`([^`]+?)`)/g;
  let last = 0;
  let match: RegExpExecArray | null;

  while ((match = re.exec(text)) !== null) {
    if (match.index > last) {
      segments.push({ text: text.slice(last, match.index) });
    }
    if (match[2]) {
      segments.push({ text: match[2], bold: true });
    } else if (match[3]) {
      segments.push({ text: match[3], italic: true });
    } else if (match[4]) {
      segments.push({ text: match[4], code: true });
    }
    last = match.index + match[0].length;
  }

  if (last < text.length) {
    segments.push({ text: text.slice(last) });
  }

  return segments;
}

function InlineText({ text }: { text: string }): React.JSX.Element {
  const segments = parseInline(text);
  return (
    <Text>
      {segments.map((seg, i) => {
        if (seg.code) {
          return (
            <Text key={i} backgroundColor={COLORS.muted} color="white">
              {` ${seg.text} `}
            </Text>
          );
        }
        if (seg.bold) {
          return (
            <Text key={i} bold>
              {seg.text}
            </Text>
          );
        }
        if (seg.italic) {
          return (
            <Text key={i} italic color={COLORS.label}>
              {seg.text}
            </Text>
          );
        }
        return <Text key={i}>{seg.text}</Text>;
      })}
    </Text>
  );
}

// --- Block renderers ---

function HeadingBlock({ level, content }: { level: number; content: string }): React.JSX.Element {
  const color = level === 1 ? COLORS.brand : level === 2 ? COLORS.purple : COLORS.info;
  return (
    <Box marginBottom={level <= 2 ? 1 : 0}>
      <Text bold color={color}>
        {content}
      </Text>
    </Box>
  );
}

function CodeBlock({ lines, lang }: { lines: string[]; lang?: string }): React.JSX.Element {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={COLORS.muted}
      paddingX={1}
      marginY={0}
    >
      {lang && (
        <Text dimColor italic>
          {lang}
        </Text>
      )}
      {lines.map((line, i) => (
        <Text key={i} color={COLORS.value}>
          {line}
        </Text>
      ))}
    </Box>
  );
}

function BlockquoteBlock({ lines }: { lines: string[] }): React.JSX.Element {
  return (
    <Box flexDirection="column" paddingLeft={1}>
      {lines.map((line, i) => (
        <Box key={i}>
          <Text color={COLORS.muted}>{"│ "}</Text>
          <InlineText text={line} />
        </Box>
      ))}
    </Box>
  );
}

function BulletListBlock({ items }: { items: string[] }): React.JSX.Element {
  return (
    <Box flexDirection="column">
      {items.map((item, i) => (
        <Box key={i}>
          <Text color={COLORS.info}>{"  ● "}</Text>
          <InlineText text={item} />
        </Box>
      ))}
    </Box>
  );
}

function OrderedListBlock({ items }: { items: string[] }): React.JSX.Element {
  return (
    <Box flexDirection="column">
      {items.map((item, i) => (
        <Box key={i}>
          <Text color={COLORS.info}>{`  ${i + 1}. `}</Text>
          <InlineText text={item} />
        </Box>
      ))}
    </Box>
  );
}

// --- Main component ---

export function Markdown({ content }: MarkdownProps): React.JSX.Element {
  const nodes = parseMarkdown(content);

  return (
    <Box flexDirection="column">
      {nodes.map((node, i) => {
        switch (node.type) {
          case "heading":
            return <HeadingBlock key={i} level={node.level!} content={node.content!} />;
          case "code_block":
            return <CodeBlock key={i} lines={node.lines!} lang={node.lang} />;
          case "blockquote":
            return <BlockquoteBlock key={i} lines={node.lines!} />;
          case "bullet_list":
            return <BulletListBlock key={i} items={node.lines!} />;
          case "ordered_list":
            return <OrderedListBlock key={i} items={node.lines!} />;
          case "paragraph":
            return (
              <Box key={i}>
                <InlineText text={node.content!} />
              </Box>
            );
          case "blank":
            return <Box key={i} height={1} />;
          default:
            return null;
        }
      })}
    </Box>
  );
}

export default Markdown;
