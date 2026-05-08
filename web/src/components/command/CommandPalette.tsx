// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal or routed-control keyboard path; preserving current interaction model.
// biome-ignore-all lint/a11y/noStaticElementInteractions: Intentional backdrop; interactive child controls and keyboard paths are handled nearby.
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { useAppStore } from "../../stores/app";
import { Kbd } from "../ui/Kbd";
import type { CommandGroup, CommandItem } from "./commandTypes";
import { fetchCatalog, useCommandItems } from "./useCommandItems";

// ── Highlight helper ───────────────────────────────────────────────────

function highlightMatch(text: string, query: string): ReactNode {
  if (!query) return text;
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const regex = new RegExp(`(${escaped})`, "gi");
  const parts = text.split(regex);
  let offset = 0;
  return parts.map((part) => {
    const key = `${part}-${offset}`;
    offset += part.length;
    // Test once and reset to avoid cumulative lastIndex drift.
    regex.lastIndex = 0;
    const isMatch =
      regex.test(part) && part.toLowerCase() === query.toLowerCase();
    regex.lastIndex = 0;
    return isMatch ? <mark key={key}>{part}</mark> : part;
  });
}

// ── Group ──────────────────────────────────────────────────────────────

interface GroupedItems {
  group: CommandGroup;
  items: { item: CommandItem; flatIdx: number }[];
}

function buildGroups(items: CommandItem[]): GroupedItems[] {
  const out: GroupedItems[] = [];
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    const last = out[out.length - 1];
    if (last && last.group === item.group) {
      last.items.push({ item, flatIdx: i });
    } else {
      out.push({ group: item.group, items: [{ item, flatIdx: i }] });
    }
  }
  return out;
}

// ── Empty state ────────────────────────────────────────────────────────

function EmptyState({ query }: { query: string }) {
  return (
    <div className="cmd-palette-empty" data-testid="cmd-palette-empty">
      {query.trim()
        ? `No results for "${query.trim()}". Try a shorter search or use the full search with Cmd+F.`
        : "Type to search agents, channels, wiki, and more — or jump to an action."}
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
}

/**
 * Full-featured command palette. Opened via Cmd+K / Ctrl+K.
 *
 * Command categories:
 * - Actions  — static shortcuts (settings, health, copy link, etc.)
 * - Agents   — open any agent from the roster
 * - Channels — jump to any channel
 * - Tasks    — open task board / specific task
 * - Wiki     — open any wiki page from the catalog (query ≥ 2 chars)
 *
 * Keyboard navigation:
 * - ArrowDown / ArrowUp — move selection
 * - Enter               — execute selected item
 * - Escape              — close the palette
 */
export function CommandPalette({ open, onClose }: CommandPaletteProps) {
  const [query, setQuery] = useState("");
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [wikiCatalog, setWikiCatalog] = useState<
    { path: string; title: string }[]
  >([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => {
    setQuery("");
    setSelectedIdx(0);
    onClose();
  }, [onClose]);

  // Fetch wiki catalog once when the palette first opens.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    fetchCatalog()
      .then((entries) => {
        if (!cancelled) setWikiCatalog(entries);
      })
      .catch(() => {
        // Degraded mode: wiki items simply won't appear.
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  // Focus input when palette opens; restore focus on close.
  useEffect(() => {
    if (!open) {
      setQuery("");
      setSelectedIdx(0);
      return;
    }
    const prevFocus = document.activeElement as HTMLElement | null;
    const id = window.requestAnimationFrame(() => inputRef.current?.focus());
    return () => {
      window.cancelAnimationFrame(id);
      if (prevFocus?.isConnected && typeof prevFocus.focus === "function") {
        prevFocus.focus();
      }
    };
  }, [open]);

  const items = useCommandItems({
    query,
    onClose: close,
    wikiCatalog,
  });

  // Clamp selection when item count changes.
  useEffect(() => {
    setSelectedIdx((idx) => Math.min(idx, Math.max(items.length - 1, 0)));
  }, [items.length]);

  // Scroll selected item into view. selectedIdx is the only dep we care
  // about; listRef.current is a mutable ref and does not participate in
  // the dependency comparison.
  // biome-ignore lint/correctness/useExhaustiveDependencies: selectedIdx drives scroll; listRef.current is a stable mutable ref excluded from dep arrays by convention.
  useEffect(() => {
    if (!listRef.current) return;
    const selected = listRef.current.querySelector<HTMLElement>(
      ".cmd-palette-item.selected",
    );
    selected?.scrollIntoView({ block: "nearest" });
  }, [selectedIdx]);

  // Keyboard handler.
  useEffect(() => {
    if (!open) return;
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Keyboard handler is a focused switch; each key branch is simple.
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopImmediatePropagation();
        close();
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((i) =>
          items.length === 0 ? 0 : (i + 1) % items.length,
        );
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((i) =>
          items.length === 0 ? 0 : (i - 1 + items.length) % items.length,
        );
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        const item = items[selectedIdx];
        if (item) item.run();
      }
    }
    // Use capture so Escape claims priority over all other open modals.
    document.addEventListener("keydown", handleKeyDown, true);
    return () => document.removeEventListener("keydown", handleKeyDown, true);
  }, [open, items, selectedIdx, close]);

  const grouped = useMemo(() => buildGroups(items), [items]);

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) close();
    },
    [close],
  );

  if (!open) return null;

  const queryTrim = query.trim();

  return (
    <div
      className="cmd-palette-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      data-testid="cmd-palette"
      onClick={handleOverlayClick}
    >
      <div className="cmd-palette-shell card">
        {/* Search input */}
        <div className="cmd-palette-input-wrap">
          <svg
            aria-hidden="true"
            focusable="false"
            className="cmd-palette-search-icon"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            className="cmd-palette-input"
            type="text"
            role="combobox"
            aria-expanded={items.length > 0}
            aria-autocomplete="list"
            aria-haspopup="listbox"
            placeholder="Jump to agent, channel, wiki, action..."
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIdx(0);
            }}
            data-testid="cmd-palette-input"
          />
        </div>

        {/* Results */}
        <div
          ref={listRef}
          className="cmd-palette-results"
          role="listbox"
          aria-label="Command palette results"
        >
          {items.length === 0 ? (
            <EmptyState query={query} />
          ) : (
            grouped.map((g) => (
              <div key={g.group} className="cmd-palette-group">
                <div className="cmd-palette-group-title" aria-hidden="true">
                  {g.group}
                </div>
                {/* biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Item row renders conditional highlight and optional fields; extracting would require prop drilling all state. */}
                {g.items.map(({ item, flatIdx }) => (
                  <button
                    key={item.id}
                    type="button"
                    role="option"
                    aria-selected={flatIdx === selectedIdx}
                    className={`cmd-palette-item${flatIdx === selectedIdx ? " selected" : ""}`}
                    onMouseEnter={() => setSelectedIdx(flatIdx)}
                    onClick={item.run}
                    data-testid={`cmd-item-${item.id}`}
                  >
                    <span className="cmd-palette-item-icon" aria-hidden="true">
                      {item.icon}
                    </span>
                    <span className="cmd-palette-item-text">
                      <span className="cmd-palette-item-label">
                        {g.group === "Wiki" || g.group === "Messages"
                          ? highlightMatch(item.label, queryTrim)
                          : item.label}
                      </span>
                      {item.desc ? (
                        <span className="cmd-palette-item-desc">
                          {g.group === "Wiki"
                            ? highlightMatch(item.desc, queryTrim)
                            : item.desc}
                        </span>
                      ) : null}
                    </span>
                    {item.meta ? (
                      <span className="cmd-palette-item-meta">{item.meta}</span>
                    ) : null}
                  </button>
                ))}
              </div>
            ))
          )}
        </div>

        {/* Footer */}
        <div className="cmd-palette-footer">
          <span>
            <Kbd size="sm">↑</Kbd>
            <Kbd size="sm">↓</Kbd> navigate
          </span>
          <span>
            <Kbd size="sm">↵</Kbd> open
          </span>
          <span>
            <Kbd size="sm">esc</Kbd> close
          </span>
        </div>
      </div>
    </div>
  );
}

/**
 * App-level host: reads open state from the store and wires the close handler.
 * Mount once in Shell alongside SearchModal, HelpModal, etc.
 */
export function CommandPaletteHost() {
  const commandPaletteOpen = useAppStore((s) => s.commandPaletteOpen);
  const setCommandPaletteOpen = useAppStore((s) => s.setCommandPaletteOpen);
  return (
    <CommandPalette
      open={commandPaletteOpen}
      onClose={() => setCommandPaletteOpen(false)}
    />
  );
}
