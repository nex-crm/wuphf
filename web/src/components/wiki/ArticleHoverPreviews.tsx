import {
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { fetchArticle } from "../../api/wiki";
import { excerptFromMarkdown } from "./articleContent";

/**
 * Hover layer for the article read view — Wikipedia's two hover behaviors:
 *
 * - **Page previews**: hovering a blue wikilink for a beat shows a card
 *   with the target article's title and its first ~40 words.
 * - **Citation popovers**: hovering a `[n]` footnote marker shows the
 *   reference text from the article's own footnote list.
 *
 * Implemented with delegated mouseover/mouseout on the article container
 * (the links are plain `<a>` elements emitted by react-markdown), so the
 * markdown pipeline stays untouched. Redlinks get no preview — there is
 * nothing to preview.
 */

export interface HoverPreviewContent {
  title?: string;
  body: string;
}

interface PreviewState extends HoverPreviewContent {
  kind: "wikilink" | "citation";
  left: number;
  top: number;
}

interface ArticleHoverPreviewsProps {
  /** The article body container the layer delegates hover events on. */
  containerRef: RefObject<HTMLElement | null>;
  /**
   * Resolves a wikilink slug to preview content. Defaults to fetching the
   * article and excerpting its first ~40 words; injectable for tests and
   * Storybook. Resolve to null to show nothing.
   */
  fetchPreview?: (slug: string) => Promise<HoverPreviewContent | null>;
  /** Hover intent delay in ms before the card shows. */
  delayMs?: number;
}

const DEFAULT_DELAY_MS = 350;
const PREVIEW_WIDTH = 320;

const previewCache = new Map<string, HoverPreviewContent | null>();

async function defaultFetchPreview(
  slug: string,
): Promise<HoverPreviewContent | null> {
  if (previewCache.has(slug)) return previewCache.get(slug) ?? null;
  try {
    const article = await fetchArticle(slug);
    const content: HoverPreviewContent = {
      title: article.title,
      body: excerptFromMarkdown(article.content),
    };
    previewCache.set(slug, content);
    return content;
  } catch {
    previewCache.set(slug, null);
    return null;
  }
}

/** Position the card near the anchor, clamped to the viewport. */
function cardPosition(anchor: Element): { left: number; top: number } {
  const rect = anchor.getBoundingClientRect();
  const viewportWidth = document.documentElement.clientWidth || 1024;
  const left = Math.max(
    8,
    Math.min(rect.left, viewportWidth - PREVIEW_WIDTH - 8),
  );
  return { left, top: rect.bottom + 6 };
}

/** Extract the citation text for a `[n]` footnote marker from the DOM. */
function citationText(anchor: HTMLAnchorElement): string | null {
  const href = anchor.getAttribute("href") ?? "";
  if (!href.startsWith("#")) return null;
  const target = document.getElementById(decodeURIComponent(href.slice(1)));
  if (!target) return null;
  const clone = target.cloneNode(true) as HTMLElement;
  // Drop the backref carets so the popover shows just the reference text.
  for (const backref of Array.from(
    clone.querySelectorAll("[data-footnote-backref]"),
  )) {
    backref.remove();
  }
  const text = (clone.textContent ?? "").trim();
  return text.length > 0 ? text : null;
}

export default function ArticleHoverPreviews({
  containerRef,
  fetchPreview = defaultFetchPreview,
  delayMs = DEFAULT_DELAY_MS,
}: ArticleHoverPreviewsProps) {
  const [preview, setPreview] = useState<PreviewState | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hoverTokenRef = useRef(0);
  const overCardRef = useRef(false);

  const cancelPending = useCallback(() => {
    hoverTokenRef.current += 1;
    if (timerRef.current) {
      globalThis.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const scheduleCitation = (anchor: HTMLAnchorElement, token: number) => {
      const body = citationText(anchor);
      if (!body) return;
      const pos = cardPosition(anchor);
      timerRef.current = globalThis.setTimeout(() => {
        if (hoverTokenRef.current !== token) return;
        setPreview({ kind: "citation", body, ...pos });
      }, delayMs);
    };

    const scheduleWikilink = (anchor: HTMLAnchorElement, token: number) => {
      const isWikilink = anchor.getAttribute("data-wikilink") === "true";
      const isBroken = anchor.getAttribute("data-broken") === "true";
      const slug = anchor.getAttribute("data-slug");
      if (!isWikilink || isBroken || !slug) return;
      const pos = cardPosition(anchor);
      timerRef.current = globalThis.setTimeout(() => {
        if (hoverTokenRef.current !== token) return;
        void fetchPreview(slug).then((content) => {
          if (hoverTokenRef.current !== token || !content) return;
          setPreview({ kind: "wikilink", ...content, ...pos });
        });
      }, delayMs);
    };

    const onOver = (ev: Event) => {
      const { target } = ev;
      if (!(target instanceof Element)) return;
      const anchor = target.closest("a");
      if (!(anchor instanceof HTMLAnchorElement)) return;

      cancelPending();
      const token = hoverTokenRef.current;

      if (anchor.hasAttribute("data-footnote-ref")) {
        scheduleCitation(anchor, token);
        return;
      }
      scheduleWikilink(anchor, token);
    };

    const onOut = (ev: Event) => {
      const { target } = ev;
      if (!(target instanceof Element)) return;
      if (!target.closest("a")) return;
      cancelPending();
      // Give the pointer a beat to land on the card before hiding.
      timerRef.current = globalThis.setTimeout(() => {
        if (!overCardRef.current) setPreview(null);
      }, 120);
    };

    container.addEventListener("mouseover", onOver);
    container.addEventListener("mouseout", onOut);
    return () => {
      container.removeEventListener("mouseover", onOver);
      container.removeEventListener("mouseout", onOut);
      cancelPending();
    };
  }, [containerRef, fetchPreview, delayMs, cancelPending]);

  if (!preview) return null;
  return (
    <div
      className={`wk-hover-preview wk-hover-preview--${preview.kind}`}
      data-testid="wk-hover-preview"
      role="tooltip"
      style={{ left: preview.left, top: preview.top, width: PREVIEW_WIDTH }}
      onMouseEnter={() => {
        overCardRef.current = true;
      }}
      onMouseLeave={() => {
        overCardRef.current = false;
        setPreview(null);
      }}
    >
      {preview.title ? (
        <div className="wk-hover-preview-title">{preview.title}</div>
      ) : null}
      <div className="wk-hover-preview-body">{preview.body}</div>
    </div>
  );
}
