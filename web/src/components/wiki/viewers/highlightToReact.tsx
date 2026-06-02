import { Fragment, type ReactNode } from "react";

/**
 * highlightToReact — convert a lowlight hast tree into React nodes WITHOUT any
 * raw-HTML injection.
 *
 * lowlight (`highlight` / `highlightAuto`) returns a hast `Root` whose tree is
 * built exclusively from text + `<span class="hljs-…">` element nodes. We walk
 * that tree recursively and emit `<span>` / string nodes via React, carrying the
 * `className` from each element's hast properties. Because the text is rendered
 * as React children (never as raw HTML), it is inherently escaped and no
 * executable markup can survive — there is nothing to sanitize and no innerHTML
 * to guard.
 *
 * Shared by SourceViewer and NotebookViewer so the converter lives in exactly
 * one place.
 *
 * The hast surface below is inlined rather than imported from the `hast`
 * package so this module depends only on its declared deps (lowlight). lowlight
 * emits text + element nodes; we only read `value`, `properties.className`, and
 * `children`. lowlight's `Root` is structurally compatible with `HastRoot`, so
 * callers pass the highlight result directly.
 */

interface HastText {
  type: "text";
  value: string;
}
interface HastElement {
  type: "element";
  tagName: string;
  properties?: {
    className?:
      | string
      | number
      | boolean
      | null
      | undefined
      | Array<string | number>;
  };
  children: HastNode[];
}
type HastNode = HastText | HastElement | { type: string };

export interface HastRoot {
  type: "root";
  children: HastNode[];
}

/**
 * Flatten a hast element's `className` property into a single space-joined
 * string suitable for React's `className`. highlight.js emits class names as a
 * string array (for example `["hljs-keyword"]`); a plain string is also
 * tolerated. Anything else yields `undefined`.
 */
function classNameOf(node: HastElement): string | undefined {
  const cls = node.properties?.className;
  if (Array.isArray(cls)) return cls.map(String).join(" ");
  if (typeof cls === "string") return cls;
  return undefined;
}

function renderNodes(nodes: HastNode[]): ReactNode[] {
  return nodes.map((node, i) => {
    if (node.type === "text") {
      return <Fragment key={i}>{(node as HastText).value}</Fragment>;
    }
    if (node.type === "element") {
      const el = node as HastElement;
      return (
        <span key={i} className={classNameOf(el)}>
          {renderNodes(el.children)}
        </span>
      );
    }
    // hast can also carry comment / doctype nodes; lowlight never emits them
    // and they have no visible rendering, so they are dropped.
    return null;
  });
}

/**
 * Convert a lowlight hast `Root` into React nodes for rendering inside a
 * `<code>` element. The returned nodes are safe to mount directly.
 */
export function highlightToReact(tree: HastRoot): ReactNode {
  return renderNodes(tree.children);
}
