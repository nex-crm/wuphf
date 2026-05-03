// Office-sheet avatar portraits for built-in agents and dynamic agents.
// Unknown slugs deterministically pick from the generated office catalog so
// newly created teammates do not fall back to the deprecated legacy sprites.

import {
  KNOWN_AVATAR_SPRITES,
  type KnownAvatarSprite,
  resolveKnownPortraitSprite,
} from "./avatarSprites.generated";

const AGENT_COLORS: Record<string, string> = {
  ceo: "#E8A838",
  eng: "#3FB950",
  gtm: "#FFA657",
  human: "#38BDF8",
  pm: "#58A6FF",
  fe: "#A371F7",
  frontend: "#A371F7",
  be: "#3FB950",
  backend: "#3FB950",
  ai: "#D2A8FF",
  "ai-eng": "#D2A8FF",
  ai_eng: "#D2A8FF",
  designer: "#F778BA",
  cmo: "#FFA657",
  cro: "#79C0FF",
  pam: "#F4B6C2",
  nex: "#56D4DD",
};

const AGENT_COLOR_ALIASES: Record<string, string> = {
  planner: "pm",
  product: "pm",
  "product-manager": "pm",
  builder: "eng",
  "founding-engineer": "eng",
  "workflow-architect": "eng",
  "automation-builder": "eng",
  growth: "gtm",
  "growth-ops": "gtm",
  monetization: "cro",
  revenue: "cro",
  invoicing: "cro",
  operator: "nex",
  ops: "nex",
  operations: "nex",
};

type Rgb = readonly [number, number, number];

function hexToRgb(hex: string): Rgb {
  return [
    Number.parseInt(hex.slice(1, 3), 16),
    Number.parseInt(hex.slice(3, 5), 16),
    Number.parseInt(hex.slice(5, 7), 16),
  ];
}

function paletteFromHexes(palette: string[]): Record<number, Rgb> {
  return Object.fromEntries(
    palette.map((hex, index) => [index + 1, hexToRgb(hex)]),
  );
}

export function getAgentColor(slug: string): string {
  const normalized = slug.trim().toLowerCase();
  const key = AGENT_COLOR_ALIASES[normalized] ?? normalized;
  return AGENT_COLORS[key] ?? proceduralAccentForSlug(key);
}

const RESERVED_DYNAMIC_AVATAR_IDS = new Set([
  "hybridCeo",
  "hybridGeneric",
  "hybridHuman",
  "hybridPam",
  "hybridPamCute",
]);

const DYNAMIC_AVATAR_IDS = Object.keys(KNOWN_AVATAR_SPRITES)
  .filter(
    (id) => id.startsWith("hybrid") && !RESERVED_DYNAMIC_AVATAR_IDS.has(id),
  )
  .sort();

const PROCEDURAL_ACCENTS = [
  "#E8A838",
  "#58A6FF",
  "#A371F7",
  "#3FB950",
  "#D2A8FF",
  "#F778BA",
  "#FFA657",
  "#79C0FF",
  "#FF7B72",
  "#56D4DD",
  "#FFD866",
  "#C9D1D9",
];

function hashSlug(slug: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < slug.length; i++) {
    h ^= slug.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

function pick(hash: number, salt: number, modulo: number): number {
  let h = hash ^ (salt * 0x9e3779b1);
  h = Math.imul(h ^ (h >>> 16), 0x85ebca6b);
  h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35);
  h ^= h >>> 16;
  return (h >>> 0) % modulo;
}

function proceduralAccentForSlug(slug: string): string {
  const hash = hashSlug(slug || "unknown");
  return (
    PROCEDURAL_ACCENTS[pick(hash, 9, PROCEDURAL_ACCENTS.length)] ?? "#56D4DD"
  );
}

function rgbToHex([r, g, b]: Rgb): string {
  return `#${[r, g, b].map((v) => Math.max(0, Math.min(255, v)).toString(16).padStart(2, "0")).join("")}`;
}

function luminance([r, g, b]: Rgb): number {
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function isSkinLike([r, g, b]: Rgb): boolean {
  return r >= 120 && g >= 70 && b <= 190 && r >= g && g >= b;
}

function blend(a: Rgb, b: Rgb, amount: number): Rgb {
  return [
    Math.round(a[0] + (b[0] - a[0]) * amount),
    Math.round(a[1] + (b[1] - a[1]) * amount),
    Math.round(a[2] + (b[2] - a[2]) * amount),
  ];
}

function buildProceduralOfficePortrait(slug: string): KnownAvatarSprite {
  const hash = hashSlug(slug || "unknown");
  const ids =
    DYNAMIC_AVATAR_IDS.length > 0 ? DYNAMIC_AVATAR_IDS : ["hybridGeneric"];
  const baseID = ids[pick(hash, 8, ids.length)];
  const base =
    KNOWN_AVATAR_SPRITES[baseID] ??
    KNOWN_AVATAR_SPRITES.hybridGeneric ??
    Object.values(KNOWN_AVATAR_SPRITES)[0];
  if (!base) {
    throw new Error("avatar sprite catalog is empty");
  }

  const accent = hexToRgb(proceduralAccentForSlug(slug));
  const tintStrength = 0.22 + pick(hash, 10, 18) / 100;
  const palette = base.palette.map((hex) => {
    const rgb = hexToRgb(hex);
    if (luminance(rgb) < 38 || isSkinLike(rgb)) {
      return hex;
    }
    return rgbToHex(blend(rgb, accent, tintStrength));
  });

  return {
    ...base,
    id: `procedural:${slug || "unknown"}:${base.id}`,
    palette,
  };
}

export function resolvePortraitSprite(slug: string): KnownAvatarSprite {
  const normalized = slug.trim().toLowerCase();
  const known = resolveKnownPortraitSprite(normalized);
  if (known) return known;

  return buildProceduralOfficePortrait(normalized);
}

export function paintPixelAvatarData(
  data: Uint8ClampedArray,
  sprite: readonly (readonly number[])[],
  palette: Record<number, Rgb>,
  cols: number,
): void {
  for (let r = 0; r < sprite.length; r++) {
    for (let c = 0; c < cols; c++) {
      const px = sprite[r]?.[c] ?? 0;
      const idx = (r * cols + c) * 4;
      if (px === 0) {
        data[idx] = 0;
        data[idx + 1] = 0;
        data[idx + 2] = 0;
        data[idx + 3] = 0;
        continue;
      }

      const rgb = palette[px] ?? ([128, 128, 128] as const);
      data[idx] = rgb[0];
      data[idx + 1] = rgb[1];
      data[idx + 2] = rgb[2];
      data[idx + 3] = 255;
    }
  }
}

/**
 * Paint a pixel-art agent avatar into an existing canvas element.
 * Known agents render from the generated avatar catalog; everything else gets
 * a deterministic generated office portrait.
 */
export function drawPixelAvatar(
  canvas: HTMLCanvasElement,
  slug: string,
  size: number,
): void {
  const avatar = resolvePortraitSprite(slug);
  const sprite = avatar.portrait;
  const palette = paletteFromHexes(avatar.palette);

  const rows = sprite.length;
  const cols = sprite[0]?.length ?? 0;
  if (rows === 0 || cols === 0) return;

  canvas.width = cols;
  canvas.height = rows;
  canvas.style.width = `${size}px`;
  canvas.style.height = `${(size * rows) / cols}px`;

  const ctx = canvas.getContext("2d");
  if (!ctx) return;

  const imgData = ctx.createImageData(cols, rows);
  paintPixelAvatarData(imgData.data, sprite, palette, cols);
  ctx.putImageData(imgData, 0, 0);
}
