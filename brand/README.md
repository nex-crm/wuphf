# WUPHF Brand Assets

Official logo, icon exports, and usage guidance for the WUPHF brand.

## The Logo

The WUPHF mark is a pixel-art "W" set inside an amber sign-frame. It echoes the WUPHF sign glowing on the back wall of the pixel office, and reads cleanly from 16px browser-tab scale up to 1024px app-store scale.

It is built from a 16-unit grid with `shape-rendering="crispEdges"`. Every size is a pixel-aligned integer multiple of that grid, so no anti-aliased blur ever shows up in exports. Zero border-radius. Zero smoothing. Zero gradients.

### Primary — `wuphf-logo.svg`

![Primary logo](png/wuphf-logo-128.png)

Amber frame, dark interior, amber W. Use this everywhere by default: browser tabs, app icons, social avatars, READMEs, docs, merch, GitHub profile, business cards. Self-contained, so it works on any background.

### Inverted — `wuphf-logo-inverted.svg`

![Inverted logo](png/wuphf-logo-inverted-128.png)

Dark W stamped on a solid amber field. Use this only when the primary would disappear or lose punch, for example:

- Placed directly on the amber accent color itself, where the primary's amber frame would vanish
- High-contrast print contexts where a single flat color reads better than a frame-and-fill
- Spots where you want the logo to feel like a stamp, sticker, or badge

If in doubt, use the primary.

## Colors

| Token | Hex | Role |
|-------|-----|------|
| `--yellow` | `#ECB22E` | WUPHF amber, primary accent |
| `--bg` | `#1A1610` | Warm near-black, primary background |

Source of truth is [DESIGN.md](../DESIGN.md). Never substitute a different amber, never use pure black.

## Files

```
brand/
  wuphf-logo.svg              — primary, scales to any size
  wuphf-logo-inverted.svg     — alternate, scales to any size
  png/
    wuphf-logo-16.png          → tab favicon
    wuphf-logo-32.png          → standard favicon
    wuphf-logo-64.png
    wuphf-logo-128.png
    wuphf-logo-180.png         → apple-touch-icon
    wuphf-logo-192.png         → Android chrome icon
    wuphf-logo-256.png
    wuphf-logo-512.png         → PWA splash
    wuphf-logo-1024.png        → App Store / marketing hero
    wuphf-logo-inverted-*.png  (same sizes)
```

## Clear Space

Leave at least 1 logo-unit of empty space on every side. At 128px that is 8px of breathing room. Do not pack the logo against text, borders, or other marks without that margin.

## Do

- Use the SVG whenever possible. It stays crisp at every size.
- Keep the amber and the dark exactly as defined.
- Let the logo sit on any solid background — it was designed to be self-contained.

## Do Not

- Do not recolor the logo outside the two defined hex values.
- Do not apply drop shadows, outer glows, or blur filters. It is pixel-art, not a skeuomorph.
- Do not round the corners, stretch the proportions, or tilt it.
- Do not place the primary logo on the amber accent color itself. Use the inverted version instead.
- Do not re-render it in a different typeface. The W is hand-pixeled, not generated from a font.

## Regenerating PNG Exports

If the SVG sources ever change, regenerate PNGs with:

```bash
cd brand
for size in 16 32 64 128 180 192 256 512 1024; do
  rsvg-convert -w $size -h $size wuphf-logo.svg -o png/wuphf-logo-${size}.png
  rsvg-convert -w $size -h $size wuphf-logo-inverted.svg -o png/wuphf-logo-inverted-${size}.png
done
```

Requires `rsvg-convert` (`brew install librsvg`).
