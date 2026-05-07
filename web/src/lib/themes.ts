/**
 * Theme registry — single source of truth for every theme the app ships.
 *
 * Adding a theme means:
 *   1. Drop the CSS file under `web/public/themes/<id>.css`.
 *   2. Append an entry to `THEMES` below.
 *
 * The `Theme` union, the switcher menu, and the loader in `RootRoute` all
 * derive their behaviour from this list.
 */

export interface ThemeSwatch {
  /** Dominant chrome color used for the half of the corner badge. */
  primary: string;
  /** Accent color used for the other half of the corner badge. */
  accent: string;
  /** Body surface color, used as the swatch border so it reads against any bg. */
  surface: string;
}

export interface ThemeDef {
  id: string;
  name: string;
  desc: string;
  swatch: ThemeSwatch;
  /** Public path served by Vite — loaded into a `<link>` tag at runtime. */
  cssPath: string;
}

export const THEMES = [
  {
    id: "nex",
    name: "Nex Light",
    desc: "Clean light. Cyan accents.",
    swatch: { primary: "#612a92", accent: "#9f4dbf", surface: "#ffffff" },
    cssPath: "/themes/nex.css",
  },
  {
    id: "nex-dark",
    name: "Nex Dark",
    desc: "Low-glare dark.",
    swatch: { primary: "#0f0f12", accent: "#9f4dbf", surface: "#1a1a1f" },
    cssPath: "/themes/nex-dark.css",
  },
  {
    id: "noir-gold",
    name: "Noir Gold",
    desc: "Black, gold leaf.",
    swatch: { primary: "#0a0a0a", accent: "#d4af37", surface: "#161616" },
    cssPath: "/themes/noir-gold.css",
  },
] as const satisfies readonly ThemeDef[];

export type Theme = (typeof THEMES)[number]["id"];

export const DEFAULT_THEME: Theme = "nex";

const THEME_IDS: ReadonlySet<string> = new Set(THEMES.map((t) => t.id));

/** Type guard for unknown values that might be persisted theme ids. */
export function isTheme(v: unknown): v is Theme {
  return typeof v === "string" && THEME_IDS.has(v);
}

/** Resolve a theme id to its definition, falling back to the default. */
export function getTheme(id: Theme): (typeof THEMES)[number] {
  const found = THEMES.find((t) => t.id === id);
  if (found) return found;
  // Manifest is non-empty and `id` is a member of the union, so this branch
  // is unreachable in practice; the explicit fallback keeps the return type
  // narrow for callers.
  return THEMES[0];
}
