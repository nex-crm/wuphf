/**
 * Breadcrumb — object path for the current view.
 *
 * Rendered inside ChannelHeader (left side) for non-channel routes.
 * Each segment is a link; the last segment also shows a copy-link button
 * on hover. Using the hash-based href directly means the link works as a
 * native anchor — no router.navigate() call needed for a deep link.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { useCallback, useEffect, useRef, useState } from "react";

import type { BreadcrumbItem } from "../../hooks/useObjectBreadcrumb";

interface BreadcrumbProps {
  items: BreadcrumbItem[];
}

export function Breadcrumb({ items }: BreadcrumbProps) {
  if (items.length === 0) return null;

  return (
    <nav
      className="breadcrumb"
      aria-label="Object breadcrumb"
    >
      {items.map((item, idx) => {
        const isLast = idx === items.length - 1;
        return (
          <span key={item.href} className="breadcrumb-segment">
            {idx > 0 && (
              <span className="breadcrumb-sep" aria-hidden="true">
                /
              </span>
            )}
            {isLast ? (
              <BreadcrumbLeaf item={item} />
            ) : (
              <a className="breadcrumb-link" href={item.href}>
                {item.label}
              </a>
            )}
          </span>
        );
      })}
    </nav>
  );
}

function BreadcrumbLeaf({ item }: { item: BreadcrumbItem }) {
  const [copied, setCopied] = useState(false);
  const resetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (resetTimerRef.current !== null) {
        clearTimeout(resetTimerRef.current);
      }
    };
  }, []);

  const handleCopy = useCallback(async () => {
    try {
      // Build the full deep-link URL by replacing the hash fragment.
      // item.href is like "#/wiki/people/nazz"; window.location gives the base.
      const url = `${window.location.origin}${window.location.pathname}${item.href}`;
      await navigator.clipboard.writeText(url);
      setCopied(true);
      if (resetTimerRef.current !== null) {
        clearTimeout(resetTimerRef.current);
      }
      resetTimerRef.current = setTimeout(() => {
        setCopied(false);
        resetTimerRef.current = null;
      }, 1800);
    } catch {
      // Clipboard not available (non-HTTPS, deny permission). Silently ignore.
    }
  }, [item.href]);

  return (
    <span className="breadcrumb-leaf">
      <a className="breadcrumb-link breadcrumb-link-active" href={item.href}>
        {item.label}
      </a>
      <button
        type="button"
        className="breadcrumb-copy-btn"
        onClick={handleCopy}
        title={copied ? "Copied!" : "Copy deep link"}
        aria-label={copied ? "Link copied" : "Copy deep link"}
      >
        {copied ? (
          <svg
            aria-hidden="true"
            focusable="false"
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M20 6 9 17l-5-5" />
          </svg>
        ) : (
          <svg
            aria-hidden="true"
            focusable="false"
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
            <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
          </svg>
        )}
      </button>
    </span>
  );
}
