# Design System — WUPHF Pixel Office Website

## Product Context
- **What this is:** The marketing/landing website for WUPHF — an open-source AI agent office tool where an AI team (CEO, PM, engineers, designer) works visibly in a shared office, claimable via one terminal command.
- **Who it's for:** Developers and technical founders who want an AI team that works in the open rather than behind an API.
- **Space/industry:** AI developer tools / agent frameworks.
- **Project type:** Isometric pixel-art marketing site. The website IS the product experience — not a description of it.

---

## Aesthetic Direction
- **Direction:** Pixel-Retro / The Office (US TV show) workplace — isometric top-down 3D view
- **Decoration level:** Expressive — 100% pixel art, no smooth vectors, no photos, no gradients
- **Mood:** A dark office where something is always happening. AI agents and The Office cast sharing the floor. Dim fluorescent lights, golden WUPHF sign glowing on the back wall, everything quietly animated. You discover the product by exploring the environment, not by reading bullet points.
- **EUREKA:** Every AI agent platform uses dark mode + gradient hero + "your AI team" headline. This site IS the office. The product concept and the website experience are unified. You don't read about WUPHF — you live in it for 30 seconds on the landing page.

---

## Color System
**Approach:** Dark mode. Amber (#ECB22E) as primary accent — the WUPHF yellow from the existing app token `--yellow`. Works dramatically on dark backgrounds.

| Token | Hex | Usage |
|-------|-----|-------|
| `--bg` | `#1A1610` | Page background (very dark warm brown) |
| `--surface` | `#242018` | Cards, panels, nav |
| `--surface-high` | `#2E2820` | Inputs, raised elements |
| `--border` | `#3A3028` | Dividers, tile outlines, outlines |
| `--text` | `#F0EBD8` | Primary text (warm off-white) |
| `--text-muted` | `#8A7D6A` | Secondary text, labels |
| `--yellow` | `#ECB22E` | WUPHF amber/gold — primary accent, sign, nav logo, CTAs |
| `--yellow-dark` | `#C49020` | Button offset shadows, active borders |
| `--yellow-glow` | `rgba(236,178,46,0.15)` | Subtle ambient glow behind the sign |
| `--blue` | `#5A9AC8` | Secondary accent, links, engineer agent (brighter for dark bg) |
| `--green` | `#5AAA7A` | CMO agent nameplate |

**Implementation aliases:** The live page may keep legacy CSS variable names while using this token system: `--bg2` maps to `--surface`, `--bg3` maps to `--surface-high`, `--text-dim` maps to `--text-muted`, and `--accent-lime` maps to `--green`. These aliases must not reintroduce purple/violet accents.

**Scene-specific:**
| Token | Hex | Usage |
|-------|-----|-------|
| `--carpet` | `#3A3228` | Office floor tile (primary) |
| `--carpet-alt` | `#302A20` | Office floor tile (alternating) |
| `--wall-scene` | `#201C14` | Back wall in isometric scene |
| `--desk-top` | `#7A5A18` | Desk top face |
| `--desk-dark` | `#5A3C08` | Desk front face |
| `--desk-side` | `#3A2404` | Desk side face |
| `--fluorescent` | `#FFFEF0` | Ceiling light elements + glow cone |

**Anti-patterns:** No purple/violet accents. No gradient backgrounds. No light-mode version. No desaturated muted amber.

---

## Typography

Three-font stack. Each has a distinct register and visual purpose. Do not substitute.

| Role | Font | Usage |
|------|------|-------|
| **Display** | `Press Start 2P` | Nav logo, WUPHF sign, UI labels, buttons, section headers |
| **Dialogue** | `VT323` | Character thought bubbles, papers on desks, in-world text |
| **Functional** | `DM Mono` | Body copy, install commands, design system docs |

**Loading:** Google Fonts CDN
```html
<link href="https://fonts.googleapis.com/css2?family=Press+Start+2P&family=VT323&family=DM+Mono:wght@400;500&display=swap" rel="stylesheet">
```

**Scale (Press Start 2P):**
- Hero/Sign: 28–32px
- Section heads: 14–16px
- UI labels, nav, buttons: 8–11px
- Tags, metadata: 6–7px

**Scale (VT323):**
- Thought bubbles: 20–28px (reads at high size, tight leading)
- In-world paper text: 16–20px

**Scale (DM Mono):**
- Body: 14–16px, line-height 1.8–1.9
- Code: 13px, monospace block with border

**Font blacklist (never use for this project):** Inter, Roboto, Helvetica, system fonts for anything visible.

---

## Spacing
- **Base unit:** 8px (one "half-pixel" in the isometric grid)
- **Scale:** `2xs=2 xs=4 sm=8 md=16 lg=24 xl=32 2xl=48 3xl=64`
- **Density:** Tight for the pixel scene (4–8px between elements), comfortable for design system docs (24–36px section margins)
- **Rule:** Everything snaps to the pixel grid. No fractional px values in the isometric scene.

---

## Layout
- **Approach:** Full-viewport isometric scene as hero. No scroll needed for the above-the-fold experience. Design system docs scroll below.
- **Scene:** Canvas-rendered isometric office
  - Grid: 9 cols × 6 rows of 60×30px diamond tiles
  - Origin: OX=420, OY=100 (adjustable for viewport)
  - Draw order: back-to-front (painter's algorithm)
- **Nav:** Sticky, minimal, 10px padding. Surface background with yellow bottom border.
- **Max content width:** 1000px (for design system docs section below scene)
- **Mobile (< 768px):** 2D side-scrolling view of office hallway. Same pixel aesthetic, same colors. Characters walk left/right. No isometric perspective. WUPHF sign still prominent on wall.
- **Border radius:** None. Zero. This is pixel art. Everything is sharp rectangles.

---

## Motion

**Hard rule: `steps()` everywhere. No `cubic-bezier`, no `ease-in-out`. Smooth animation breaks the pixel art illusion.**

| Type | Duration | Easing | Usage |
|------|----------|--------|-------|
| Character idle bob | 300ms/frame, 4-frame loop | `steps(1)` | All characters breathe/sway slightly |
| Flash (clickable glow) | 800ms total | `steps(1)` | Binary on/off amber box-shadow pulse |
| Thought bubble appear | 150ms | `steps(3)` | Scale 0→1 in 3 discrete steps. Pop, not slide. |
| Drawer open | 100ms | `steps(2)` | Slide reveal in 2 frames |
| Button :active | Instant | none | translate(5px, 5px) + shadow collapse. No CSS transition. |

**CSS pattern for pixel animations:**
```css
animation: my-anim 800ms steps(1) infinite;
```

---

## Isometric Scene Specification

### Coordinate system
```
iso_screen_x = OX + (gx - gy) * TW / 2
iso_screen_y = OY + (gx + gy) * TH / 2
```
Where `TW=60`, `TH=30`, `OX=420`, `OY=100`.

### Back wall
- Full-width dark panel `#201C14`, height = OY + 30px
- 4 fluorescent fixtures: `#FFFEF0` with rgba glow cone below
- WUPHF sign: `#0E0C08` panel, `#ECB22E` border + text, subtle `rgba(236,178,46,0.08)` inner glow, `ctx.shadowColor` amber glow on text

### Floor tiles
- 9×6 diamond grid. Alternating `#3A3228` / `#302A20`.
- Tile outline: `#2A2418` at 0.5px.

### Character positions
| Character | Grid position | Notes |
|-----------|---------------|-------|
| Pam Beesly | (3.5, 0.5) | Reception, in front of WUPHF sign |
| Michael Scott | (6.5, 1.5) | Wandering, background right |
| Dwight Schrute | (1, 3) | At his desk, left side |
| Jim Halpert | (3, 3) | At his desk, center — looks at viewer |
| Kevin Malone | (5.5, 4) | Near snack jar, wide sprite |
| Creed Bratton | (0.5, 5) | Far corner, doing something unexplained |
| CEO Agent | (5.5, 2) | Amber nameplate (#ECB22E) |
| Engineer Agent | (2.5, 4) | Blue nameplate (#5A9AC8) |
| CMO Agent | (4.2, 3.2) | Green nameplate (#5AAA7A) |

### Interactivity
1. **Flashing drawer:** First desk item (reception desk, right side). Pulses amber. Carries "Click Me!" tooltip above it (amber background, dark text). On click: reveals `"One command. One office. ./wuphf"` in an amber-bordered panel.
2. **All clickable items:** Stepped amber `box-shadow` pulse. No smooth glow.
3. **Character click:** Shows thought bubble with character quote. VT323 font. Amber border. 5s auto-dismiss. Click character again to dismiss.
4. **Hidden messaging:** Multiple clickable items throughout the scene, each revealing product copy in the environment.

### Hidden messaging map
| Location | Click to reveal |
|----------|----------------|
| Reception drawer (first, flashes) | `"One command. One office. ./wuphf"` |
| Paper on Pam's desk | `"CEO, PM, engineers — all visible, all working."` |
| Conference room whiteboard | Architecture diagram — agent routing + broker |
| Agent monitor screen | `"Open source. MIT license. go build -o wuphf"` |
| Plant by window | `"Unlike Ryan Howard's WUPHF, this one works."` |
| Break room fridge | Full install command |
| Stapler in jello (Dwight area) | Easter egg — Dwight rage quit message |
| Dundie on shelf | `"Best AI Office, 2025. (Self-awarded.)"` |
| Kevin's snack jar label | `"No tokens wasted on pleasantries."` |

### Character thought bubbles (click to trigger)
| Character | Quote |
|-----------|-------|
| Pam Beesly | "WUPHF!" |
| Michael Scott | "I'm not superstitious, but I am a little stitious." |
| Dwight Schrute | "Bears. Beets. Battlestar Galactica." |
| Jim Halpert | "How the turntables..." |
| Kevin Malone | "... (stares at snacks)" |
| Creed Bratton | "Nobody steals from Creed Bratton and gets away with it. The website is fine." |
| CEO Agent | "Routing task to engineering team. ETA: 3 minutes." |
| Engineer Agent | "Implementing feature... 47% complete." |
| CMO Agent | "Drafting launch post. You will not believe this lede." |

---

## Component Specs

### Pixel Button (primary CTA)
```css
font-family: 'Press Start 2P'; font-size: 9px;
background: #ECB22E; color: #1A1610;
border: none; padding: 14px 24px;
box-shadow: 5px 5px 0 #C49020;
/* NO transition */
```
`:active` → `transform: translate(5px, 5px); box-shadow: none;`

### Thought Bubble
```css
background: #242018; border: 3px solid #ECB22E;
font-family: 'VT323'; font-size: 20–28px;
color: #F0EBD8;
box-shadow: 4px 4px 0 #C49020;
/* steps(3) appear animation */
```
- Speaker name: `Press Start 2P`, 6px, `#ECB22E`
- Triangle tail: `border-top-color: #ECB22E`

### Nav
```css
background: #242018;
border-bottom: 3px solid #C49020;
```
- Logo: `Press Start 2P`, 11px, `#ECB22E`
- Links: `Press Start 2P`, 7px, `#8A7D6A` → `#ECB22E` on hover

---

## Anti-Patterns (do not use)
- Smooth CSS transitions or ease-in-out animations in the pixel scene
- `border-radius` on any element inside or matching the pixel scene
- Any light-mode color values
- Purple, violet, or blue as the primary accent
- Gradient backgrounds or gradient buttons
- Photo backgrounds or non-pixel-art imagery
- Inter, Roboto, or system fonts for visible text
- Traditional hero copy with a CTA above the fold

---

## Decisions Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-04-15 | Dark mode instead of beige/light | Amber (#ECB22E) pops on dark. On beige it washes out. Dark also fits "AI agents working late" narrative and differentiates from every SaaS site using white + blue. |
| 2026-04-15 | WUPHF Yellow (#ECB22E) as primary accent | Picked up from existing app `--yellow` token. Consistent with product branding. |
| 2026-04-15 | Press Start 2P + VT323 + DM Mono | Three-font system: pixel UI, in-world dialogue, functional prose. Each has a distinct register. |
| 2026-04-15 | Zero hero copy in first viewport | All messaging is environmental. The product concept (visible AI team) IS the website experience. |
| 2026-04-15 | steps() animations only — hard rule | Smooth CSS transitions break the pixel art illusion. 16-bit game feel requires discrete frames. |
| 2026-04-15 | Isometric 3D view as hero layout | Isometric is unmistakable and distinctive. No other AI tool website does this. The product IS an office, so the website IS an office. |
| 2026-04-15 | The Office cast + WUPHF agents on same floor | Office cast provides instant recognition and humor. AI agents demonstrate the product inline. The joke writes itself — they coexist. |
