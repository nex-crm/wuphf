// WUPHF Video Design Tokens
// Pixel-accurate Slack Dark Mode clone

export const slack = {
  sidebar: "#3F0E40",
  sidebarHover: "rgba(255,255,255,0.04)",
  sidebarActive: "#1164A3",
  sidebarText: "rgba(255,255,255,0.7)",
  sidebarTextActive: "#FFFFFF",
  sidebarBorder: "rgba(255,255,255,0.08)",
  presence: "#2BAC76",

  bg: "#1A1D21",
  bgWarm: "#222529",
  bgCard: "#1A1D21",
  text: "#D1D2D3",
  textSecondary: "#ABABAD",
  textTertiary: "#8B8D90",

  accent: "#1D9BD1",
  accentWarm: "#1264A3",
  accentBg: "rgba(29, 155, 209, 0.1)",
  green: "#007A5A",
  greenPresence: "#2BAC76",
  red: "#E01E5A",
  yellow: "#ECB22E",
  blue: "#1D9BD1",

  border: "#35373B",
  borderLight: "#2C2D30",
} as const;

// Shared color aliases
export const colors = {
  bgBlack: "#000000",
  bg: slack.bg,
  bgCard: slack.bgWarm,
  bgTerminal: "#0D1117",
  bgSidebar: slack.sidebar,

  textBright: "#FFFFFF",
  text: slack.text,
  textDim: slack.textSecondary,
  textMuted: slack.textTertiary,

  accent: slack.accent,
  green: slack.green,
  red: slack.red,
  yellow: slack.yellow,
  blue: slack.blue,

  // Agent colors (from getAgentColor in web/index.html)
  ceo: "#E8A838",
  eng: "#3FB950",
  gtm: "#FFA657",
  human: "#38BDF8",
  pm: "#58A6FF",
  fe: "#A371F7",
  be: "#3FB950",
  ai: "#D2A8FF",
  designer: "#F778BA",
  cmo: "#FFA657",
  cro: "#79C0FF",
} as const;

// Agent emojis (from AGENTS array in web/index.html)
export const agentEmojis: Record<string, string> = {
  ceo: "👔",
  eng: "⚙️",
  gtm: "💰",
  pm: "📋",
  fe: "🎨",
  be: "⚙️",
  ai: "🧠",
  designer: "✏️",
  cmo: "📣",
  cro: "💰",
  human: "👤",
};

export const fonts = {
  sans: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif",
  mono: "'SFMono-Regular', Menlo, Monaco, Consolas, monospace",
} as const;

export const FPS = 30;
export const sec = (s: number) => Math.round(s * FPS);

// Starter pack agents
export const starterAgents = [
  { slug: "ceo", name: "CEO",               color: colors.ceo, emoji: "👔" },
  { slug: "eng", name: "Founding Engineer",  color: colors.eng, emoji: "⚙️" },
  { slug: "gtm", name: "GTM Lead",          color: colors.gtm, emoji: "💰" },
] as const;

// Pack display with emojis
export const packs = [
  { name: "Starter Team",     agents: 3,  emoji: "🚀", desc: "CEO + Engineer + GTM" },
  { name: "Founding Team",    agents: 8,  emoji: "🏢", desc: "Full autonomous company" },
  { name: "Coding Team",      agents: 4,  emoji: "💻", desc: "Tech Lead + FE + BE + QA" },
  { name: "Lead Gen Agency",  agents: 4,  emoji: "📈", desc: "AE + SDR + Research + Content" },
] as const;
