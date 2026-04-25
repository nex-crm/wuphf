// Procedural pixel-art avatar builder.
// Composes a 14x14 sprite from modular layers — hair, headwear, facial feature,
// neck accessory — plus per-slug palette picks (accent, hair color, skin tone).
// Layer picks are derived from a hash of the agent slug so the same slug always
// produces the same avatar, but different slugs get distinct looks.
//
// Palette slots match pixelAvatar.ts:
//   0 = transparent, 1 = outline, 2 = skin, 3 = accent, 4 = hair,
//   5 = prop, 6 = highlight.

export type PixelGrid = number[][];
export type Rgb = readonly [number, number, number];
export type Palette = Record<number, Rgb>;

const ROWS = 14;
const COLS = 14;

// Base body — face, torso, arms. No hair; hair layers add it.
const BASE: PixelGrid = [
  [0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0],
  [0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0],
  [0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0],
  [0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0],
  [0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0],
  [0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0],
  [0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0],
  [0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0],
  [0, 0, 2, 2, 3, 3, 3, 3, 3, 3, 2, 2, 0, 0],
  [0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 1, 0, 0],
  [0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0],
  [0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0],
  [0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0],
  [0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0],
];

// A layer is a sparse overlay. Cells with -1 are noop. Non-negative cells are
// stamped onto the base (0 = transparent, 1..6 = palette index).
const NO = -1;
type Layer = number[][];

function emptyLayer(): Layer {
  return Array.from({ length: ROWS }, () =>
    Array.from({ length: COLS }, () => NO),
  );
}

// ── Hair layers (rows 0–2) ──────────────────────────────────────────────

function hairClassic(): Layer {
  const l = emptyLayer();
  for (let c = 4; c <= 9; c++) l[1][c] = 4;
  return l;
}

function hairSpiky(): Layer {
  const l = emptyLayer();
  l[0][4] = 4;
  l[0][6] = 4;
  l[0][8] = 4;
  for (let c = 4; c <= 9; c++) l[1][c] = 4;
  return l;
}

function hairSidePart(): Layer {
  const l = emptyLayer();
  for (let c = 4; c <= 9; c++) l[1][c] = 4;
  l[1][7] = 2; // part line shows skin
  return l;
}

function hairBald(): Layer {
  return emptyLayer();
}

function hairAfro(): Layer {
  const l = emptyLayer();
  for (let c = 3; c <= 10; c++) l[0][c] = 4;
  for (let c = 2; c <= 11; c++) l[1][c] = 4;
  l[2][3] = 4;
  l[2][10] = 4;
  return l;
}

function hairMohawk(): Layer {
  const l = emptyLayer();
  l[0][6] = 4;
  l[0][7] = 4;
  l[1][6] = 4;
  l[1][7] = 4;
  return l;
}

function hairLongSides(): Layer {
  const l = emptyLayer();
  for (let c = 4; c <= 9; c++) l[1][c] = 4;
  l[2][3] = 4;
  l[2][10] = 4;
  l[3][3] = 4;
  l[3][10] = 4;
  return l;
}

const HAIR_STYLES = [
  hairClassic,
  hairSpiky,
  hairSidePart,
  hairBald,
  hairAfro,
  hairMohawk,
  hairLongSides,
];

// ── Headwear layers (rows 0–2) — stamped AFTER hair ────────────────────

function headwearNone(): Layer {
  return emptyLayer();
}

function headwearCap(): Layer {
  const l = emptyLayer();
  for (let c = 3; c <= 10; c++) l[0][c] = 5;
  for (let c = 3; c <= 10; c++) l[1][c] = 5;
  for (let c = 2; c <= 11; c++) l[2][c] = 1; // brim outline
  return l;
}

function headwearHeadband(): Layer {
  const l = emptyLayer();
  for (let c = 4; c <= 9; c++) l[2][c] = 3; // accent-colored band
  return l;
}

function headwearBeanie(): Layer {
  const l = emptyLayer();
  for (let c = 4; c <= 9; c++) l[0][c] = 3;
  for (let c = 3; c <= 10; c++) l[1][c] = 3;
  return l;
}

function headwearVisor(): Layer {
  const l = emptyLayer();
  for (let c = 2; c <= 11; c++) l[2][c] = 5;
  return l;
}

const HEADWEAR_STYLES = [
  headwearNone,
  headwearNone, // weight "none" more heavily so avatars aren't all hatted
  headwearCap,
  headwearHeadband,
  headwearBeanie,
  headwearVisor,
];

// ── Facial feature (rows 3–5) ──────────────────────────────────────────

function faceNone(): Layer {
  return emptyLayer();
}

function faceGlasses(): Layer {
  const l = emptyLayer();
  // Round glasses framing the eyes at cols 5 and 8.
  l[3][4] = 1;
  l[3][5] = 5;
  l[3][6] = 1;
  l[3][7] = 1;
  l[3][8] = 5;
  l[3][9] = 1;
  return l;
}

function faceMustache(): Layer {
  const l = emptyLayer();
  for (let c = 5; c <= 8; c++) l[5][c] = 4;
  return l;
}

function faceBeard(): Layer {
  const l = emptyLayer();
  l[4][4] = 4;
  l[4][9] = 4;
  for (let c = 5; c <= 8; c++) l[5][c] = 4;
  return l;
}

const FACE_STYLES = [
  faceNone,
  faceNone,
  faceNone, // weight "none" to keep most faces clean
  faceGlasses,
  faceMustache,
  faceBeard,
];

// ── Neck accessory (rows 6–9) ──────────────────────────────────────────

function neckNone(): Layer {
  return emptyLayer();
}

function neckTie(): Layer {
  const l = emptyLayer();
  l[6][6] = 6;
  l[6][7] = 6;
  l[7][6] = 6;
  l[7][7] = 6;
  return l;
}

function neckBadge(): Layer {
  const l = emptyLayer();
  l[9][4] = 6;
  return l;
}

const NECK_STYLES = [neckNone, neckNone, neckTie, neckBadge];

// ── Palette pools ──────────────────────────────────────────────────────

const ACCENT_POOL: string[] = [
  "#E8A838", // amber
  "#58A6FF", // blue
  "#A371F7", // violet
  "#3FB950", // green
  "#D2A8FF", // lilac
  "#F778BA", // pink
  "#FFA657", // orange
  "#79C0FF", // sky
  "#FF7B72", // coral
  "#56D4DD", // teal
  "#FFD866", // yellow
  "#C9D1D9", // slate
];

const HAIR_POOL: Rgb[] = [
  [36, 32, 30], // near-black
  [74, 52, 38], // dark brown
  [139, 90, 43], // brown
  [200, 155, 90], // blond
  [170, 60, 45], // auburn
  [200, 200, 200], // silver
  [236, 178, 46], // amber (match brand)
  [180, 120, 200], // lilac (fun)
];

const SKIN_POOL: Rgb[] = [
  [248, 220, 190],
  [235, 195, 160],
  [210, 165, 125],
  [175, 125, 90],
  [140, 95, 65],
  [100, 65, 45],
];

// ── Hash ───────────────────────────────────────────────────────────────

// Deterministic FNV-1a 32-bit hash — same slug → same avatar.
function hashSlug(slug: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < slug.length; i++) {
    h ^= slug.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

// Pull a sequence of independent indices from one hash by mixing in a salt.
function pick(hash: number, salt: number, modulo: number): number {
  let h = hash ^ (salt * 0x9e3779b1);
  h = Math.imul(h ^ (h >>> 16), 0x85ebca6b);
  h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35);
  h ^= h >>> 16;
  return (h >>> 0) % modulo;
}

// ── Hex → RGB ──────────────────────────────────────────────────────────

function hexToRgb(hex: string): Rgb {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return [r, g, b];
}

// ── Public API ─────────────────────────────────────────────────────────

export interface ProceduralSprite {
  grid: PixelGrid;
  palette: Palette;
  accentHex: string;
}

export function buildProceduralSprite(slug: string): ProceduralSprite {
  const hash = hashSlug(slug || "unknown");

  const hairStyle = HAIR_STYLES[pick(hash, 1, HAIR_STYLES.length)];
  const headwearStyle = HEADWEAR_STYLES[pick(hash, 2, HEADWEAR_STYLES.length)];
  const faceStyle = FACE_STYLES[pick(hash, 3, FACE_STYLES.length)];
  const neckStyle = NECK_STYLES[pick(hash, 4, NECK_STYLES.length)];

  const accentHex = ACCENT_POOL[pick(hash, 5, ACCENT_POOL.length)];
  const hairColor = HAIR_POOL[pick(hash, 6, HAIR_POOL.length)];
  const skinColor = SKIN_POOL[pick(hash, 7, SKIN_POOL.length)];

  const grid: PixelGrid = BASE.map((row) => row.slice());
  const layers = [hairStyle(), headwearStyle(), faceStyle(), neckStyle()];
  for (const layer of layers) {
    for (let r = 0; r < ROWS; r++) {
      for (let c = 0; c < COLS; c++) {
        const v = layer[r][c];
        if (v !== NO) grid[r][c] = v;
      }
    }
  }

  const palette: Palette = {
    1: [36, 32, 30],
    2: skinColor,
    3: hexToRgb(accentHex),
    4: hairColor,
    5: [180, 170, 155],
    6: [255, 255, 255],
  };

  return { grid, palette, accentHex };
}

export function getProceduralAccent(slug: string): string {
  const hash = hashSlug(slug || "unknown");
  return ACCENT_POOL[pick(hash, 5, ACCENT_POOL.length)];
}
