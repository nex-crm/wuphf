/**
 * stickyPinRegistry — shared sticky-pin detection for sidebar section
 * headers.
 *
 * Each `.sidebar-section-title-bar` uses `position: sticky` so it pins
 * to the top of `.sidebar-scroll` while its section is on screen. We
 * want CSS to fade in a drop shadow only while a header is actually
 * pinned, which means comparing the header's `getBoundingClientRect().top`
 * against the scroll root's. Doing that per-header attaches N scroll
 * listeners that each re-read the same root rect — quadratic-ish work
 * per scroll event with O(N) layout reads interleaved with attribute
 * writes (the textbook layout-thrash shape).
 *
 * This registry collapses that to:
 *   - one `scroll` listener per scroll root (added when the first header
 *     registers, removed when the last unregisters);
 *   - one rAF per scroll burst;
 *   - one `getBoundingClientRect()` on the root + N on the headers, all
 *     reads first, all `data-stuck` writes second (no read-after-write).
 *
 * A `ResizeObserver` watches the scroll root so collapse/expand and
 * viewport resizes also recompute.
 */

interface RootState {
  headers: Set<HTMLElement>;
  onScroll: () => void;
  observer: ResizeObserver;
  rafHandle: number | null;
}

const roots = new WeakMap<HTMLElement, RootState>();

function flush(scrollRoot: HTMLElement, state: RootState): void {
  state.rafHandle = null;
  if (state.headers.size === 0) return;
  // Read phase — all rect reads happen before any DOM write so we don't
  // force a layout flush in the middle of the batch.
  const rootTop = scrollRoot.getBoundingClientRect().top;
  const flips: Array<[HTMLElement, boolean]> = [];
  for (const header of state.headers) {
    const tbTop = header.getBoundingClientRect().top;
    flips.push([header, tbTop - rootTop < 1]);
  }
  // Write phase.
  for (const [header, stuck] of flips) {
    const next = stuck ? "true" : "false";
    if (header.dataset.stuck !== next) header.dataset.stuck = next;
  }
}

function schedule(scrollRoot: HTMLElement, state: RootState): void {
  if (state.rafHandle !== null) return;
  state.rafHandle = requestAnimationFrame(() => flush(scrollRoot, state));
}

/**
 * Register a sticky header against its scroll root. Returns a cleanup
 * function that removes the header from the registry and tears down
 * the shared listener when no headers remain.
 */
export function registerStickyHeader(
  scrollRoot: HTMLElement,
  header: HTMLElement,
): () => void {
  let state = roots.get(scrollRoot);
  if (!state) {
    const partial: Partial<RootState> = { headers: new Set(), rafHandle: null };
    const onScroll = () => {
      // partial.headers is always set because we always populate it
      // before assigning the listener.
      const s = roots.get(scrollRoot);
      if (s) schedule(scrollRoot, s);
    };
    const observer = new ResizeObserver(() => {
      const s = roots.get(scrollRoot);
      if (s) schedule(scrollRoot, s);
    });
    partial.onScroll = onScroll;
    partial.observer = observer;
    state = partial as RootState;
    roots.set(scrollRoot, state);
    scrollRoot.addEventListener("scroll", onScroll, { passive: true });
    observer.observe(scrollRoot);
  }
  state.headers.add(header);
  // Initial layout may not be settled when registerStickyHeader runs
  // (the component just mounted); schedule a frame so the sticky pin
  // resolves before we measure.
  schedule(scrollRoot, state);

  return () => {
    const current = roots.get(scrollRoot);
    if (!current) return;
    current.headers.delete(header);
    if (current.headers.size === 0) {
      if (current.rafHandle !== null) cancelAnimationFrame(current.rafHandle);
      scrollRoot.removeEventListener("scroll", current.onScroll);
      current.observer.disconnect();
      roots.delete(scrollRoot);
    }
  };
}
