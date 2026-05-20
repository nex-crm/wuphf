import type { ComponentProps, ReactElement, ReactNode } from "react";

import type { CalloutType } from "./parseCallout";

/**
 * Renders an Obsidian-flavored callout block.
 *
 * The actual detection lives in `parseCallout.ts`'s remark plugin, which
 * tags blockquote nodes with `data-callout-*` properties. This component
 * is wired in via the react-markdown `blockquote` override and renders
 * a callout when those data attributes are present, falling through to
 * a plain blockquote otherwise.
 */

const TYPE_LABELS: Record<CalloutType, string> = {
  note: "Note",
  info: "Info",
  tip: "Tip",
  important: "Important",
  warning: "Warning",
  caution: "Caution",
};

const KNOWN_TYPES: ReadonlySet<string> = new Set<CalloutType>([
  "note",
  "info",
  "tip",
  "important",
  "warning",
  "caution",
]);

type BlockquoteProps = ComponentProps<"blockquote">;

function resolveType(raw: unknown): CalloutType {
  if (typeof raw === "string" && KNOWN_TYPES.has(raw)) {
    return raw as CalloutType;
  }
  return "note";
}

/**
 * react-markdown blockquote override. Detects the `data-callout` attribute
 * (set by `calloutRemarkPlugin`) and renders a `<Callout>` aside; otherwise
 * renders a plain `<blockquote>` so non-callout quotes are unaffected.
 */
export function CalloutBlockquote(props: BlockquoteProps): ReactElement {
  const record = props as Record<string, unknown>;
  if (record["data-callout"] !== "true") {
    return <blockquote {...props} />;
  }
  const type = resolveType(record["data-callout-type"]);
  const title =
    typeof record["data-callout-title"] === "string"
      ? (record["data-callout-title"] as string)
      : "";
  const fold = record["data-callout-fold"];
  const folded = fold === "open" || fold === "closed";
  const defaultOpen = fold === "open";
  return (
    <Callout
      type={type}
      title={title}
      folded={folded}
      defaultOpen={defaultOpen}
    >
      {props.children}
    </Callout>
  );
}

interface CalloutProps {
  type: CalloutType;
  title: string;
  folded: boolean;
  defaultOpen: boolean;
  children: ReactNode;
}

export default function Callout({
  type,
  title,
  folded,
  defaultOpen,
  children,
}: CalloutProps): ReactElement {
  const label = TYPE_LABELS[type];
  const headerClass = "wk-callout-header";
  const bodyClass = "wk-callout-body";
  const headerInner = (
    <>
      <span className="wk-callout-label">{label}</span>
      {title ? <span className="wk-callout-title">{title}</span> : null}
    </>
  );
  if (folded) {
    return (
      <aside
        className={`wk-callout wk-callout-${type} wk-callout-folded`}
        data-callout-type={type}
        aria-label={`${label} callout`}
      >
        <details open={defaultOpen}>
          <summary className={headerClass}>{headerInner}</summary>
          <div className={bodyClass}>{children}</div>
        </details>
      </aside>
    );
  }
  return (
    <aside
      className={`wk-callout wk-callout-${type}`}
      data-callout-type={type}
      aria-label={`${label} callout`}
    >
      <div className={headerClass}>{headerInner}</div>
      <div className={bodyClass}>{children}</div>
    </aside>
  );
}
