import { useEffect, useRef } from "react";

/**
 * Tracks whether the element's vertical content overflows the viewport,
 * setting `data-overflow="true|false"` on it. Used by the sidebar to only
 * show the bottom fade mask when there is actually content below the fold.
 */
export function useOverflow<T extends HTMLElement = HTMLDivElement>() {
  const ref = useRef<T | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const update = () => {
      const has = el.scrollHeight > el.clientHeight + 1;
      el.dataset.overflow = has ? "true" : "false";
      const atEnd = el.scrollTop + el.clientHeight >= el.scrollHeight - 1;
      el.dataset.scrollEnd = has && atEnd ? "true" : "false";
    };

    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    const mo = new MutationObserver(update);
    mo.observe(el, { childList: true, subtree: true, characterData: true });
    el.addEventListener("scroll", update, { passive: true });

    return () => {
      ro.disconnect();
      mo.disconnect();
      el.removeEventListener("scroll", update);
    };
  }, []);

  return ref;
}
