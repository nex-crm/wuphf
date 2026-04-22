// WUPHF Video Design Tokens — refreshed 2026-04-22 to match current web app.
// Sources: web/src/styles/global.css + wiki.css + messages.css + layout.css.
// No more slack-dark-purple anything — the app is light-paper + purple-sidebar now.

export const FPS = 30;
export const sec = (s: number) => Math.round(s * FPS);

// ─── Nex.System ramps (mirrored from web/src/styles/global.css:11-58) ──
export const neutral = {
  950: "#1e1f1f",
  900: "#28292a",
  800: "#323334",
  700: "#434647",
  600: "#575a5c",
  500: "#686c6e",
  400: "#85898b",
  300: "#aeb1b2",
  200: "#cfd1d2",
  100: "#e9eaeb",
  50: "#f2f2f3",
  10: "#f8f8f9",
} as const;

export const tertiary = {
  500: "#612a92",
  400: "#9f4dbf",
  300: "#cf72d9",
  200: "#ffb3e6",
  100: "#ffebfc",
} as const;

export const olive = {
  500: "#3f4224",
  400: "#969640",
  300: "#d4db18",
  200: "#eef679",
  100: "#f8ffdb",
} as const;

export const cyan = {
  500: "#069de4",
  400: "#00ccff",
  300: "#46f7fd",
  200: "#a3fcff",
  100: "#e5feff",
} as const;

export const success = {
  500: "#0d5935",
  400: "#03a04c",
  300: "#35da79",
  200: "#a3ebbb",
  100: "#e9fbef",
} as const;

export const error = {
  500: "#8c1727",
  400: "#d1261a",
  300: "#ff9a8f",
  200: "#ffe4e0",
  100: "#ffeeeb",
} as const;

export const warning = {
  500: "#994200",
  400: "#ce6b09",
  300: "#ffb647",
  200: "#f7deab",
  100: "#fbf5dc",
} as const;

// ─── Semantic aliases (mirrors the app's :root vars) ──
export const colors = {
  // Page surfaces
  bg: "#FFFFFF",
  bgWarm: neutral[50],
  bgSubtle: neutral[10],
  bgCard: "#FFFFFF",
  bgBlack: "#000000",
  bgTerminal: "#0D1117",

  // Text
  text: neutral[900],
  textSecondary: neutral[500],
  textTertiary: neutral[400],
  textDisabled: neutral[300],
  textBright: "#FFFFFF",
  textDim: neutral[500],
  textMuted: neutral[400],

  // Chrome
  border: neutral[100],
  borderLight: neutral[50],
  borderDark: neutral[200],
  borderStrong: neutral[300],

  // Primary purple (app accent)
  accent: tertiary[400],
  accentWarm: tertiary[500],
  accentBg: tertiary[100],

  // Sidebar (WUPHF purple chrome)
  sidebar: tertiary[500],        // #612a92
  bgSidebar: tertiary[500],      // legacy alias used by old scenes
  sidebarDeep: "#4a1f70",        // slightly darker sibling used in inner shadow
  sidebarText: "rgba(255,255,255,0.82)",
  sidebarTextMuted: "rgba(255,255,255,0.55)",
  sidebarActive: "rgba(255,255,255,0.14)",
  sidebarBorder: "rgba(255,255,255,0.08)",

  // Active app item uses cyan in the sidebar
  sidebarAppActive: cyan[400],
  sidebarAppActiveFg: "#062a36",

  // Channel active highlight (olive-yellow, matches app's "current channel" pill)
  channelActiveBg: "rgba(246,215,77,0.22)",
  channelActiveText: "rgba(255,255,255,0.95)",

  // Semantic
  green: success[400],
  greenBg: success[100],
  red: error[400],
  redBg: error[100],
  yellow: warning[400],
  yellowBg: warning[100],
  blue: cyan[500],
  blueBg: cyan[100],

  // Message chrome
  msgAvatarBg: neutral[50],
  msgTokenBg: "rgba(18,100,163,0.08)",
  msgTokenBorder: "rgba(18,100,163,0.18)",
  msgTokenText: tertiary[400],
  mentionBg: "#FDF1E2",
  mentionText: "#D4700E",

  // Wiki surface (mirrors web/src/styles/wiki.css)
  wkPaper: "#FFFFFF",
  wkPaperWarm: neutral[10],
  wkPaperDark: neutral[50],
  wkText: neutral[900],
  wkMuted: neutral[500],
  wkTertiary: neutral[400],
  wkBorder: neutral[100],
  wkWikilink: olive[500],
  wkWikilinkBroken: error[400],
  wkAmber: olive[400],
  wkAmberBg: olive[200],
  wkAmberBanner: olive[100],

  // Agent accent colors (mirrors getAgentColor in web/src/lib)
  ceo: "#E8A838",
  gtm: "#FFA657",
  eng: "#3FB950",
  pm: "#58A6FF",
  designer: "#F778BA",
  fe: "#A371F7",
  be: "#3FB950",
  ai: "#D2A8FF",
  cmo: "#FFA657",
  cro: "#79C0FF",
  human: "#38BDF8",
} as const;

export const fonts = {
  sans: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif",
  serif: "'Source Serif 4', ui-serif, Georgia, 'Times New Roman', serif",
  display: "'Fraunces', ui-serif, Georgia, 'Times New Roman', serif",
  mono: "'Geist Mono', 'SFMono-Regular', Menlo, Monaco, Consolas, monospace",
} as const;

export const radius = {
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  pill: 9999,
} as const;

// Starter pack agents (current web config)
export const starterAgents = [
  { slug: "ceo", name: "CEO",              role: "lead",        color: colors.ceo,      emoji: "👔" },
  { slug: "gtm", name: "GTM Lead",         role: "gtm",         color: colors.gtm,      emoji: "💰" },
  { slug: "eng", name: "Founding Engineer", role: "engineering", color: colors.eng,      emoji: "⚙️" },
  { slug: "pm",  name: "Product Manager",   role: "product",     color: colors.pm,       emoji: "📋" },
  { slug: "designer", name: "Designer",     role: "design",      color: colors.designer, emoji: "✏️" },
] as const;

// Pack display with emojis
export const packs = [
  { name: "Starter Team",     agents: 3, emoji: "🚀", desc: "CEO, Engineer, GTM" },
  { name: "Founding Team",    agents: 5, emoji: "🏢", desc: "Full autonomous office" },
  { name: "Coding Team",      agents: 4, emoji: "💻", desc: "Tech Lead, FE, BE, QA" },
  { name: "Lead Gen Agency",  agents: 4, emoji: "📈", desc: "AE, SDR, Research, Content" },
] as const;

// Agent emoji map — kept for components that display emoji alongside avatar.
export const agentEmojis: Record<string, string> = {
  ceo: "👔",
  gtm: "💰",
  eng: "⚙️",
  pm: "📋",
  designer: "✏️",
  fe: "🎨",
  be: "⚙️",
  ai: "🧠",
  cmo: "📣",
  cro: "💰",
  human: "👤",
};

// ─── Legacy `slack` export — compat shim for older scenes during migration.
// Any Scene still importing `slack.*` reads from the new light tokens so it
// renders on the refreshed palette instead of the old dark-purple chrome.
// Scheduled for removal once every scene uses `colors`.
export const slack = {
  sidebar: colors.sidebar,
  sidebarHover: "rgba(255,255,255,0.06)",
  sidebarActive: colors.sidebarActive,
  sidebarText: colors.sidebarText,
  sidebarTextActive: colors.textBright,
  sidebarBorder: colors.sidebarBorder,
  presence: success[400],
  bg: colors.bg,
  bgWarm: colors.bgWarm,
  bgCard: colors.bgCard,
  text: colors.text,
  textSecondary: colors.textSecondary,
  textTertiary: colors.textTertiary,
  accent: colors.accent,
  accentWarm: colors.accentWarm,
  accentBg: colors.accentBg,
  green: colors.green,
  greenPresence: success[400],
  red: colors.red,
  yellow: warning[400],
  blue: cyan[500],
  border: colors.border,
  borderLight: colors.borderLight,
} as const;
