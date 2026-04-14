# How to make a demo video (that doesn't look like AI slop)

A field guide for producing a 30-90 second product demo using Remotion + Eleven
Labs. This exists because shipping a good demo is harder than it looks, and most
of the mistakes are predictable.

Nothing here is specific to any one product. Swap in your own positioning, voice,
and colors. The workflow is what matters.

---

## Stack

- **Remotion** for the video composition (React-based, renders to MP4)
- **Eleven Labs** for narration and music (text-to-speech + sound-generation)
- **ffmpeg** for audio concatenation and crossfades
- Optional: **Sumo**, **Suno**, **Udio** if you prefer those for music

You need: Node 18+, ffmpeg, an Eleven Labs API key.

---

## Phase 0: Intake before code

Answer these before you open an editor. Wrong answers here cost hours later.

1. **Product and positioning**
   - What is the product? One sentence.
   - Who is the ICP? (Founders, RevOps, devs, designers, consumers.)
   - What is the ONE thing the video needs to communicate? (Not three. One.)
   - What is the CTA? (Install, signup, star, demo.)

2. **Tone**
   - Dry documentary, cinematic hero, mockumentary comedy, technical explainer,
     energetic demo. Pick one. Do not mix two.

3. **Visual references**
   - Where is the real UI? Point at the app URL or the key HTML/CSS file.
   - What are the exact brand colors? Hex values.
   - Are there real avatars, logos, emojis used in the product?

4. **Constraints**
   - Target duration (default 45-60s).
   - Target aspect ratio (16:9 landscape for YouTube/X, 9:16 for social reels).
   - Anything you do NOT want named?

---

## Phase 1: Script first, visuals second

Before any Remotion code, write the script as a table:

| # | Time | Scene | On screen | Narration |

**Length discipline:** narration lines under 8 seconds each. At natural pace
(~2.5 words/sec), that is ~20 words max per scene. If it sounds long when you
read it aloud, it is.

**Narration rules:**
- One idea per line. Do not stack benefits.
- Active voice. Specific nouns.
- No AI vocab: delve, robust, nuanced, unlock, empower, revolutionize, seamless.
- Jokes land best in the opener and the closer.

**Humor toolkit (if the tone is comedic):**
- Callback: set up a phrase early, pay it off late.
- Flip: set up an expectation, subvert with one word.
- Rule of three: two straight, third absurd.
- Dry observation: "This test suite takes longer than the feature it tests."
- Relatable corporate frustrations: "per my last message", "raise an IT ticket",
  "OKRs that never get done", "circling back".

Get script sign-off before writing code. You will iterate 5-10 times on visuals
regardless — you do not want to iterate on the script too.

---

## Phase 2: Set up Remotion

```bash
mkdir -p video && cd video
npm init -y
npm install --save-exact remotion @remotion/cli @remotion/player react react-dom typescript @types/react
```

Create the minimum structure:

```
video/
├── tsconfig.json
├── remotion.config.ts
├── src/
│   ├── index.ts          # registerRoot(Root)
│   ├── Root.tsx          # <Composition id="Demo" .../>
│   ├── Demo.tsx          # main composition
│   ├── theme.ts          # colors, fonts, timing helpers
│   ├── components/       # Terminal, ChatMessage, TypeWriter, etc.
│   └── scenes/           # Scene1Title.tsx, Scene2Command.tsx, etc.
├── public/audio/         # narration, music, SFX
└── out/                  # rendered MP4s
```

Add to `package.json`:

```json
"scripts": {
  "studio": "remotion studio src/index.ts",
  "render": "remotion render src/index.ts Demo out/demo.mp4",
  "preview": "remotion preview src/index.ts"
}
```

Standard composition: 1920x1080, 30fps, H.264.

---

## Phase 3: Clone the real product UI

Do NOT invent a generic dark theme. Find the actual stylesheet and mirror it.

- Pull the exact hex values from the product's CSS.
- Use the same font stack.
- Match avatar sizes, corner radii, padding.
- Keep the signature color. If the product is known for one color, anchor the
  scenes that show the product around that color.

In `theme.ts`, export two objects:
- `brand` — the product's exact values
- `colors` — semantic aliases (bg, text, textDim, agent colors) that forward to `brand`

**Common pitfall:** during a theme refactor, scenes still reference old tokens
(`colors.textBright`, etc.). If a token goes missing, React renders
`undefined` as CSS which falls back to inherit or black. You get invisible text
that is undetectable in the code. Always keep old aliases forwarding to new values,
or do a project-wide find-replace.

---

## Phase 4: Text sizing discipline

Video is rendered at 1920x1080 but watched on phones at 375px. Text that reads
fine on your monitor disappears on a phone.

Minimum sizes for 1920x1080:

| Element | Minimum | Ideal |
|---------|---------|-------|
| Hero title | 120px | 180-220px |
| Scene title | 40px | 60-92px |
| Body / subtitle | 28px | 36-44px |
| Message body | 18px | 22-26px |
| Sidebar labels | 14px | 16-20px |

**Rule:** if you are debating whether a size is too small, it is. Double it.

When your reviewer says "text too small everywhere", do not make spot fixes.
Audit every scene. Widen sidebars too — cramped 260px sidebars force truncated
text. 340-380px is more honest.

---

## Phase 5: Avoid the AI slop patterns

These patterns make a video instantly recognizable as AI-generated:

1. **The 3-column feature grid.** Icon-in-circle + bold title + 2-line description
   repeated 3x symmetrically. The #1 tell. Use sequential full-screen panels
   instead.
2. **Purple-to-violet gradients on white.** Dead giveaway.
3. **Everything centered.** Break symmetry. Left-align at least 2-3 scenes.
4. **Uniform bubbly radius everywhere.** Vary the radii intentionally.
5. **Decorative blobs and wavy dividers.** If a section feels empty, it needs
   better content, not decoration.
6. **Generic hero copy.** "Welcome to X", "Unlock the power of...", "Your
   all-in-one solution for...". Use specific user outcomes.
7. **Cookie-cutter rhythm.** Hero → 3 features → testimonials → pricing → CTA,
   each section same height.
8. **Emoji-only decoration.** Rockets in headlines as filler.
9. **Fade-and-slide for every entrance.** Boring. Mix in springs, overshoot,
   drift, scale-from-anchor.

**Positive replacements:**
- Full-screen sequential panels. One idea at a time. 2.5-3s each.
- Left-aligned editorial headlines. Hero number fills the left, chart fills the
  right.
- Subtle dot-grid textures + radial brand-color glows for depth.
- Hand-drawn wobble on strikethroughs.
- Confetti burst on a delight moment, not everywhere.

---

## Phase 6: Motion with personality

Build these reusable components once:

- `DotGrid` — subtle dot pattern, 4-6% opacity, optional drift
- `RadialGlow` — soft brand-color atmosphere behind hero elements
- `TypingDots` — three bouncing dots for streaming messages
- `Confetti` — burst of colored particles with gravity
- `TypeWriter` — character-by-character reveal for terminals
- `ChatMessage` — matches the product's exact message chrome, supports
  `isStreaming` (cursor), `isReply` (indent with thread line), `mentions`
  (colored @-tag highlights)

**Easing beats duration.** Use `Easing.out(Easing.cubic)` for entrances
(feels responsive). Use `spring({ damping: 10-14, stiffness: 140-200 })` for
hero elements that need to punch in.

**Only animate `transform` and `opacity`.** Never `width`, `height`, `top`,
`left`. They trigger layout and look janky.

---

## Phase 7: Narration (Eleven Labs)

**Voice selection by tone:**

| Tone | Voice name | Voice ID |
|------|-----------|----------|
| **Deadpan sarcastic narrator** (default, free tier) | Daniel - Steady Broadcaster | `onwK4e9ZLuTAKqWW03F9` |
| Dramatic movie-trailer | Bill - Wise, Mature, Balanced | `pqHfZKP75CvOlQylNhV4` |
| Down-to-earth casual | Chris - Charming, Down-to-Earth | `iP95p4xoKVk53GoZ742B` |
| Laid-back classy | Roger - Laid-Back, Casual, Resonant | `CwhRBWXzGAHq8TQ4Fs17` |
| Deep resonant | Brian - Deep, Resonant and Comforting | `nPczCjzI2devNBz1zQrb` |
| Warm storyteller | George - Warm, Captivating Storyteller | `JBFqnCBsd6RMkjVDRZzb` |
| Dominant firm | Adam - Dominant, Firm | `pNInz6obpgDQGcFmaJgB` |
| Energetic demo | Laura - Enthusiast, Quirky Attitude | `FGY2WhTYpPnr7TX5Dg9X` |
| Smooth trustworthy | Eric - Smooth, Trustworthy | `cjVigY5qzO86Huf0OWal` |

**Daniel** is the default for deadpan sarcastic narration. British BBC
newsreader authority delivered flat lands jokes through unimpressed weight —
think Arrested Development narrator, Nathan Fielder energy. Works across
free tier with eleven_v3 for best results.

**Bill** is the alternative for full theatrical movie-trailer drama. Older
American, crisp gravitas. Use when you want Don LaFontaine "In a world..."
energy instead of dry comedy.

**If you have a paid Eleven Labs plan:** the shared voice library has better
options. Top picks for dramatic narrators:
- **Flint - Deep, Raspy, and Warm** (`qAZH0aMXY8tw1QufPN0D`) — "Commanding Presence"
- **Sully - Mature, Deep and Intriguing** (`wAGzRVkxKEs8La0lmdrE`)
- **Julian - Deep, Rich and Mature** (`7p1Ofvcwsv7UBPoFNcpI`, British)
- **Frank - Wise, Deep and Motivational** (`V2bPluzT7MuirpucVAKH`)
- **William - Deep, Engaging Storyteller** (`fjnwTZkKtQOJaYzGLa6n`, British)
- **Grandfather Joe** (`0lp4RIz96WD1RUtvEu3Q`) — "timeless charm of a nature
  documentary narrator"

Library voices require `paid_plan_required`. Free tier falls back to premade.

**Model choice — three tiers:**

1. **`eleven_v3`** — best for comedic timing. Supports inline audio tags like
   `[dry]`, `[sarcastic]`, `[pause]`, `[sigh]`, `[whispers]` that actually
   control delivery. Works with premade voices. Use for comedy demos. Costs
   ~2-3x the characters of multilingual_v2.
2. **`eleven_multilingual_v2`** — solid fallback, natural prosody, 1x char cost.
3. **`eleven_turbo_v2_5`** — avoid. Fast and cheap but sounds machine-like.

**Voice settings cheat sheet — the settings matter more than the voice:**

| Goal | stability | style | Why |
|------|-----------|-------|-----|
| **Deadpan / sarcastic** | 0.65-0.75 | **0.0** | High stability = monotone. Zero style = no theatrics. THE counterintuitive key. |
| Dramatic movie trailer | 0.25 | 0.75 | Low stability = expressive range. High style = commanding delivery. |
| Natural conversational | 0.5 | 0.3 | Balanced for product demos without tone skew. |
| Warm storytelling | 0.55 | 0.45 | Some expression without melodrama. |

Always `use_speaker_boost: true` for bass weight.
Always `similarity_boost: 0.75-0.9` for voice identity stability.

**The critical mistake:** cranking `style` up to make a voice sound "more
dramatic" kills deadpan comedy. High style = theatrical = no comic timing.
For dry humor, style must be at or near zero — the joke lands because the
voice sounds unimpressed.

**API call per line:**

```bash
curl -s "https://api.elevenlabs.io/v1/text-to-speech/${VOICE_ID}" \
  -H "xi-api-key: ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "...",
    "model_id": "eleven_multilingual_v2",
    "voice_settings": {
      "stability": 0.25,
      "similarity_boost": 0.9,
      "style": 0.75,
      "use_speaker_boost": true
    }
  }' \
  --output "public/audio/narration-sceneN.mp3"
```

Tuning:
- Stability 0.55-0.7 = natural prosody with consistency (default)
- Stability < 0.5 = dramatic but inconsistent across runs
- Style 0.3-0.5 = adds expression without over-acting

**After generating, always measure duration:**

```bash
ffprobe -v quiet -show_entries format=duration -of csv=p=0 file.mp3
```

If a clip is longer than its scene, shorten the narration and regenerate. Or
extend the scene by 1-2s if the extra dwell time serves the content (dramatic
narrators take longer).

**Parallel generation hits rate limits.** Don't `curl &` all 7 scenes in
parallel — Eleven Labs returns `concurrent_limit_exceeded` for most of them.
Generate sequentially. 7 scenes × ~5s each ≈ 35s total. Worth the wait.

**Free tier has 10,000 characters per month.** eleven_v3 costs ~2-3x per
character versus multilingual_v2. A full 60s demo narration runs about
500-800 v3 characters. Budget your renders — each iteration burns quota.

Check remaining quota:

```bash
curl -s "https://api.elevenlabs.io/v1/user/subscription" \
  -H "xi-api-key: $API_KEY" | jq
```

If you run out mid-demo, fall back to `eleven_multilingual_v2` for the
remaining scenes. The drop-off in quality is modest for non-comedic lines.

---

## Writing for sarcasm and comedic timing

Eleven Labs does not parse `<em>` or emphasis markers. The only way to force
a sarcastic beat is through **punctuation as pacing**.

Techniques that actually land:

| Technique | Example | What it does |
|-----------|---------|--------------|
| Comma before the punchline | `"a team of agents, who actually show up"` | Adds a half-beat pause, lifts emphasis to "actually" |
| Em-dash for pivot | `"Like OKRs — except these actually get done"` | Dramatic ironic pivot |
| Ellipsis for suspense | `"No... per my last message"` | Longer pause, sets up the irony |
| Isolated comma words | `"where you can, actually, watch"` | Forces comedic slow-down on the middle word |
| Sentence break for deadpan | `"Or not. Who cares. I was just here to narrate."` | Each period = a full beat. Drains emotion on purpose. |

**Capitalization for emphasis does not work.** `"actually SHIPS"` is read the
same as `"actually ships"`. Use punctuation instead.

**The fourth-wall break.** Nat-Geo-narrator comedy peaks when the narrator
acknowledges the absurdity of narrating. End the video with something like:
`"Or not. Who cares. I was just here to narrate."` — pure deadpan gold on a
deep voice.

### eleven_v3 audio tags (only with v3 model)

When using `eleven_v3` (not multilingual_v2), you can insert inline tags to
control delivery. Treat them like stage directions:

```
"Unlike your last standup, [pause] everyone here [dry] actually ships."
"They notice patterns. Propose fixes. You say yes. [dry] Like OKRs,
 except these actually get done."
"The worst idea in The Office could become the best in AI. [pause] [dry]
 Or not. Who cares. I was just here to narrate."
```

Supported tags include: `[dry]`, `[sarcastic]`, `[pause]`, `[sigh]`,
`[whispers]`, `[excited]`, `[whispering]`, `[laughs]`.

One tag per sentence is usually enough. Stacking `[pause] [dry]` before a
punchline gives it the maximum comedic beat.

---

## Phase 8: Music

Eleven Labs `sound-generation` endpoint, max 30s per call. Generate 2-3 clips
and concat with ffmpeg crossfade.

**Prompts that work:**
- Dry product: "soft upbeat background music for tech product demo, light piano
  melody with gentle electronic beats, warm and minimalist modern feel"
- Comedy: "quirky upbeat office comedy background music with plucked acoustic
  guitar and light snappy percussion, playful and bouncy"
- Epic/cinematic: "epic cinematic orchestral music with sweeping strings and
  brass fanfare, heroic and triumphant"
- Ambient tech: "ambient electronic background for tech startup, subtle synths,
  minimal percussion, slightly dreamy"

```bash
curl -s "https://api.elevenlabs.io/v1/sound-generation" \
  -H "xi-api-key: ${API_KEY}" -H "Content-Type: application/json" \
  -d '{"text":"<prompt>","duration_seconds":22}' \
  --output public/audio/bg-music-a.mp3
```

Concat 3 clips with crossfade:

```bash
ffmpeg -y -i a.mp3 -i b.mp3 -i c.mp3 \
  -filter_complex "[0:a]afade=t=in:st=0:d=2[a];[2:a]afade=t=out:st=18:d=4[c];[a][1:a][c]concat=n=3:v=0:a=1[out]" \
  -map "[out]" -t 62 bg-music.mp3
```

**Volume:** mix background music at `volume={0.04}` to `{0.08}`. Anything
louder competes with narration.

---

## Phase 9: Notification sounds and transitions

Real product sounds beat generic stings. Common references:
- Slack knock (two quick hollow taps)
- iPhone tri-tone (three ascending notes)
- Discord ping (high bright chime)
- Linear whoosh (soft sweep)

Generate with Eleven Labs sound-generation at 0.5-1s duration. Use at 0.2-0.4
volume, placed right when a message appears.

Scene transitions: subtle whoosh (~0.8s) at 0.15-0.25 volume, placed 3 frames
before the cut. Louder than that and it feels like a stock template.

---

## Phase 10: Render, review, revise

```bash
npm run render
open out/demo.mp4
```

**Do not trust single renders.** Check specific frames first:

```bash
npx remotion still src/index.ts Demo /tmp/scene-4.png --frame=680
```

Then Read `/tmp/scene-4.png` to verify. Do this for every scene and major
state. Full render is 2-3 min. Still render is 10-30 sec.

**Keep the studio open.** `npm run studio` shows a live preview with hot
reload. Faster than `still` for most tweaks.

**Expected iterations: 5-10.** Common issues and fixes:

- **Invisible text (black on black):** missing color tokens. Grep all
  `colors.XXX` references, ensure each exists.
- **Text too small:** double it. Then double again if reviewers still complain.
- **Scene feels slow:** compress. Move narration earlier, shorten visual holds.
- **Music too loud:** drop volume 50%. Again.
- **Motion feels dead:** add spring-based entrances, drift on secondary
  elements (sine-wave position modulation), pulse on status dots.
- **Transitions abrupt:** whoosh SFX 3 frames before cuts, crossfade opacity.

---

## Phase 11: Gut check before shipping

Watch the final and ask:

1. If I saw this on social media, would I think "AI-generated" or "real product"?
2. Is there a 3-column feature grid? (Rebuild it.)
3. Is every scene centered? (Left-align 2-3 of them.)
4. Does every entrance fade-and-slide-up? (Replace 2-3 with springs.)
5. Do the colors match the real UI? (Fix if not.)
6. Does the narration sound corporate? (Rewrite with the humor toolkit.)
7. Is the music trying too hard? (Lower volume or regenerate.)

**The audience test:** show it to someone outside the project. If their first
reaction is "cool product" rather than "cool video", you won.

---

## Scene recipes

Common scene types. Pick what the script needs.

1. **Cold Open / Title.** Brand name, tagline, brand glow, subtle texture.
2. **The Command.** Terminal with typed install, output, browser preview sliding up.
3. **Meet the Team / Feature Lineup.** Asymmetric cluster with spring entrances.
   Cycle through variants if the product has them.
4. **It Works.** Product UI cloned closely, messages/updates streaming in,
   side counter ticking.
5. **The Redirect / Steering.** DM or direct-input panel showing mid-task
   change. Live stdout preview. Pulsing LIVE dot.
6. **The System Learns.** Card with a proposed change, Approve/Reject buttons,
   confetti burst on approve.
7. **What Makes It Work.** Sequential full-screen panels (NOT a 3-col grid).
   One idea at a time, each with unique visual treatment.
8. **The Close.** Terminal with install command, tagline, repo URL pill, brand glow.

---

## Tone presets

Ask yourself which fits:

- **Mockumentary comedy** (The Office style): warm storyteller voice, quirky
  plucked music, dry narrator, workplace jokes. B2B tools where the audience
  shares a pain.
- **Cinematic hero** (Apple keynote): British documentary voice, orchestral
  music, reverent pacing, zero jokes. Launches and premium positioning.
- **Technical explainer** (NPR): smooth trustworthy voice, ambient tech music,
  matter-of-fact narration, specific numbers. Dev tools.
- **Energetic demo** (Loom): enthusiast voice, upbeat indie music, fast pacing,
  quick cuts. Consumer products.
- **Silent motion graphics:** no narration, music + on-screen text. Social
  clips where audio plays muted.

Default to mockumentary comedy if the ICP has corporate pain points to
commiserate about. Default to cinematic hero for funding-stage announcements.

---

## Shipping checklist

- [ ] Duration matches target (±5s)
- [ ] Every narration clip fits inside its scene
- [ ] No text under minimum size thresholds
- [ ] No 3-column feature grid. No all-centered layout. No all-fade-slide motion.
- [ ] Brand colors match the real product UI
- [ ] Music volume under narration (~0.05)
- [ ] SFX at plausible volume (~0.25-0.4)
- [ ] Render time under 5 minutes
- [ ] File size under 10MB at 60s
- [ ] Reviewed a full preview, not just stills
- [ ] No names you were asked to avoid

---

## Hard-won lessons

- **Iterate on script before code.** A bad script makes 5 bad renders.
- **Render stills, not full videos, during iteration.** 10-30s vs 2-3 min.
- **Narration Sequence must be longer than the audio.** If the narration clip
  is 6.6s and the Sequence is only 6s, Remotion cuts the audio at 6s. Always
  set `durationInFrames` to at least 1s longer than the measured audio.
- **Dramatic narrators are slower.** Budget +20-30% for style 0.7+ voice
  settings. An 8-second narration at normal pacing becomes 10 seconds with
  dramatic pauses.
- **Scene duration follows narration, not the other way around.** When the
  narrator needs to breathe for a joke, extend the scene by 1-2s. Don't
  squeeze a dramatic line into a too-short visual.
- **Let the closing line breathe.** The last 2-3 seconds of the video should
  hold on the final shot. Give the narrator time to deliver the final
  deadpan beat before the visual changes.
- **No jumpy motion on opening cards.** Shakes, wobbles, and spring
  overshoots on the very first scene make viewers feel like they arrived
  mid-video. Let the opening settle. Save the kinetic motion for later scenes.
- **Knowledge-graph-style visuals need screen space.** If the graph has 6+
  nodes, it cannot share the screen with a headline. Split the canvas: text
  on the left half, visualization on the right half.
- **Full-screen sequential panels need ~4s each** to be readable.
  2-3 seconds per panel feels rushed even if the text is short.
- **Parallel API calls to Eleven Labs hit rate limits.** Sequential only.
- **v1 is never the final.** Budget for 3-5 major changes after first review.
- **Log the feedback.** When a reviewer says "text too small" a third time,
  acknowledge the pattern and make systemic changes, not spot fixes.

A good demo feels like it was made by someone who has used the product for a
year and cares whether viewers get it. A bad demo feels like a template filled
in by committee.

The difference is not effort. It is taste. Re-watch demos from companies whose
products you respect. Copy the energy, not the layout.

When in doubt, cut a scene. When still in doubt, cut a word.
