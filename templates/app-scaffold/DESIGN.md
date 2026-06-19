# DESIGN — making a WUPHF App look DESIGNED, not generated

AI_RULES.md keeps the app correct and shippable. This file keeps it from looking
like every other AI-generated tool: default Mantine on white, a `<Title>`, a card
pile, gray-on-gray. The bar: a teammate glancing at the live preview should think
"someone made this on purpose," not "an LLM bolted Mantine onto a table."

You are NOT building a free-form website. You have a fixed kit (Mantine on a white,
light-scheme surface), one screen, no router, no external fonts, no external images.
So the design discipline below is the bold-aesthetic advice from real design
practice ADAPTED to "make one focused Mantine data tool feel crafted" — expressed
through the levers you actually have: the `MantineProvider` theme, the spacing and
radius scale, `Title`/`Text` hierarchy, one restrained accent, and `Group`/`Stack`
composition. Every rule here is achievable inside the sandbox. None of it asks for
a font, a CDN, or an image the CSP forbids.

## 1. Commit to a direction for THIS app

Generic AI UI is the sign of a choice unmade. Before you write JSX, decide the
tool's character in one sentence and let it drive every later call: an exec digest
is calm, typographic, generous whitespace, one quiet accent; an ops console is
dense, monospaced numerals, tight rows, status color doing the work; a triage queue
is scannable, left-anchored, strong row rhythm. Pick one. Don't average them into
the default. State the direction in your channel narration ("this is a calm
read-down digest, so I'm going wide-leading and quiet") so the choice is visible.

## 2. Set a real theme — never ship default Mantine

> ENFORCED: the publish step rejects an app that does not render inside
> `MantineProvider` or import `@mantine/core`. The way to look custom is to
> OVERRIDE the theme below — NOT to drop Mantine and hand-roll a parallel CSS
> system. (If the human explicitly asked for a non-Mantine app, put
> `// wuphf-allow: no-mantine — <why>` in `src/main.tsx`.)

Default Mantine is a recognizable look. Override it once in `main.tsx` via
`createTheme` so the whole app inherits a deliberate scale, radius, and accent.
This is the single highest-leverage move. A restrained example:

```tsx
import { MantineProvider, createTheme } from "@mantine/core";

const theme = createTheme({
  primaryColor: "indigo",          // ONE accent. Not Mantine's default blue-for-everything.
  primaryShade: { light: 6 },
  defaultRadius: "md",             // pick ONE radius (sm | md) and hold it; don't mix.
  fontFamily:
    'system-ui, -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
  fontFamilyMonospace:
    'ui-monospace, SFMono-Regular, Menlo, "Cascadia Mono", monospace',
  headings: {
    fontWeight: "650",
    sizes: {
      h1: { fontSize: "1.65rem", lineHeight: "1.2" },
      h2: { fontSize: "1.3rem", lineHeight: "1.25" },
      h3: { fontSize: "1.05rem", lineHeight: "1.3" },
    },
  },
  // Tighten the default spacing scale so the layout has rhythm, not Mantine air.
  spacing: { xs: "0.5rem", sm: "0.75rem", md: "1rem", lg: "1.5rem", xl: "2.25rem" },
});

<MantineProvider forceColorScheme="light" theme={theme}>
```

Keep `forceColorScheme="light"` and the provider shape from AI_RULES.md — only the
`theme` is yours. Choose `primaryColor` to fit the tool's world (a finance tool ≠ a
support inbox); use one of Mantine's named palettes rather than inventing hex you
then have to maintain. Set the accent ONCE here; don't hand-color every component.

## 3. Typographic hierarchy is the layout

Hierarchy is carried by type, not by boxes. Three tiers, no more: a page title
(`<Title order={2}>`), section/label text, and body/data text. Make the steps
obvious — a too-timid h2 reads as "no hierarchy."

- One real title. Don't stack a `Title` + a `Text` subtitle + a `Badge` + an icon
  into a hero. Title, one line of dimmed context, move on.
- Use `fw`, `size`, and `c="dimmed"` for rank — not five font sizes. Labels:
  `<Text size="xs" c="dimmed" fw={600}>`. Numbers/IDs: `ff="monospace"` so columns
  align and data reads as data.
- Keep reading width sane — the scaffold caps `.app` near 880px; keep prose narrow,
  let only true tables go wide. Long text columns get `lineClamp` so a row never
  blows out its height.

## 4. Spacing rhythm and composition — earn the card

Structure should encode something true about the content, not decorate it. Vary
spacing for rhythm: generous space ABOVE a section title, tight space between a
label and its value. Use the theme scale (`gap="xs" | "sm" | "md" | "lg"`), never
raw pixels, so the rhythm is consistent.

- **Cards are the lazy answer.** A pile of identical `<Card>`s is the #1 AI tell.
  Reach for `<Card withBorder>` only when an item is a genuine standalone object.
  For a list, a bordered `<Table>` or a `<Stack gap={0}>` of divider-separated rows
  reads cleaner and denser than card confetti.
- Compose with intent: `<Group justify="space-between">` to push a count or action
  to the far edge; `<Stack>` for vertical flow; an asymmetric two-column
  `<Grid>`/`<SimpleGrid>` (e.g. a wide table + a narrow summary rail) beats a
  centered single column for anything with a primary and a secondary region.
- One framing border on the main surface beats a border on every element. Let
  whitespace and alignment do the separating.

## 5. Color: restrained, with meaning

Tinted-neutral surface + ONE accent that carries ≤10% of the pixels. Color must
mean something — never decorate.

- Status/semantics only: green = done, blue = running, red = blocked/failed,
  yellow = pending. Use `variant="light"` badges so color reads without shouting.
  The scaffold's `statusColor()` is the right pattern — keep it.
- The accent (`primaryColor`) belongs on the ONE primary action and active states,
  nothing else. Secondary actions: `variant="default"` or `variant="subtle"`.
- No gradients, no glassmorphism, no rainbow of badge colors, no colored card
  backgrounds. A near-white surface with disciplined neutral borders
  (`var(--mantine-color-gray-3)`) and one accent looks more expensive than color
  everywhere.

## 6. Motion: subtle, optional, never decorative

Extra animation reads as AI-generated. Default to none. Mantine's built-ins are
enough: `highlightOnHover` on tables, a `<Loader size="sm">` instead of bare
"Loading…", a `<Transition mount={...}>` on a panel that appears after an action,
`<Skeleton>` for content-shaped loading. No bounce, no elastic, no scroll-triggered
reveals. If it doesn't clarify state, leave it out.

## 7. Real states are part of the design

The empty / loading / error / not-connected states are not afterthoughts — they're
what the human sees first and most often. The worked example in `App.tsx` shows all
four; match that bar. An empty state is a `<Text c="dimmed">` that says what will
appear and why, not a blank screen. A not-connected integration renders a calm
connect-state, not a red error. Every guarded bridge call gets a designed fallback.

## Anti-patterns — if you ship one of these, it looks AI-made

- Default-themed Mantine: no `theme` override, Mantine's default blue everywhere.
- A `<Title>` "Dashboard" + dimmed subtitle hero over a single table. (Generic
  SaaS hero.)
- A grid of identical `<Card>`s where a `<Table>` or row list belongs. (Card pile.)
- Placeholder copy: "Welcome", "Dashboard", "Items", "Data". Name the actual thing
  ("Unread by sender", "Tasks blocked 3+ days").
- Five font sizes / five badge colors / mixed radii — no decided scale.
- Centered single column when the content has a clear primary + secondary region.
- Gradient text, glassmorphism, drop shadows on everything, animated reveals.
- A loading spinner with no empty/error/not-connected sibling.

## Pre-ship checklist (run before `register_app`)

- [ ] `main.tsx` has a `createTheme` override — primaryColor, one radius, heading
      scale, tightened spacing. Not default Mantine.
- [ ] One real `<Title>`; hierarchy carried by type (`fw`/`size`/`c="dimmed"`),
      three tiers max, no kitchen-sink hero.
- [ ] One accent color; status colors mean status; `variant="light"` badges; no
      gradients/glass/colored card backgrounds.
- [ ] Lists are tables or divider rows, NOT a pile of identical cards. Cards only
      for genuinely standalone objects.
- [ ] Spacing uses the theme scale (`gap`/`p` tokens), not raw px; rhythm varies
      (more above titles, tight within pairs); numbers/IDs are monospaced.
- [ ] Layout fits the content — asymmetric split when there's a primary+secondary
      region, not a reflexive centered column.
- [ ] Real, specific copy — no "Dashboard"/"Welcome"/"Items" placeholders.
- [ ] Empty / loading / error / not-connected states are all present and designed.
- [ ] Could a teammate tell a human designed this? If "AI made that" is undeniable,
      it failed — fix the direction before publishing.
