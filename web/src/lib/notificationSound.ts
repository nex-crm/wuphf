/**
 * Inbox notification chime — synthesized with the Web Audio API (no asset,
 * no MIME, no decoding). A bright three-note ascending arpeggio (A major:
 * A5 · C#6 · E6) with a soft octave overtone per note for a bell-like body.
 *
 * It is deliberately loud. Now that agent questions and blocking approvals
 * stay in their origin chat instead of popping a modal on every surface,
 * this chime is the human's main office-wide "something needs you" signal —
 * so it leans attention-grabbing rather than polite.
 *
 * Lazy AudioContext init. A suspended context (autoplay policy, or a tab
 * that was backgrounded) is resumed BEFORE the chime is scheduled, so the
 * first ding after focus returns is played rather than silently dropped on
 * a synchronous state check.
 */

let cachedContext: AudioContext | null = null;

/**
 * Peak amplitude of each note (0–1). Loud-but-not-clipping: the staggered,
 * fast-decaying notes sum to roughly ~0.45 at the destination, well under
 * the 1.0 clip ceiling. Exported so a test can guard against silent
 * regressions back to a barely-audible level.
 */
export const CHIME_PEAK = 0.34;

function ensureContext(): AudioContext | null {
  if (cachedContext) return cachedContext;
  if (typeof window === "undefined") return null;
  const Ctor =
    window.AudioContext ??
    (window as Window & { webkitAudioContext?: typeof AudioContext })
      .webkitAudioContext;
  if (!Ctor) return null;
  cachedContext = new Ctor();
  return cachedContext;
}

function emitChime(ctx: AudioContext): void {
  const now = ctx.currentTime;

  // Shared output with a touch of headroom so the stacked notes never clip.
  const master = ctx.createGain();
  master.gain.setValueAtTime(0.9, now);
  master.connect(ctx.destination);

  // Bright, friendly A-major arpeggio. Each note carries a soft octave
  // overtone so "louder" reads as fuller rather than harsher.
  const notes = [880, 1108.73, 1318.51]; // A5, C#6, E6
  const step = 0.085; // stagger between note onsets
  const ring = 0.28; // per-note ring-out

  for (let i = 0; i < notes.length; i++) {
    const freq = notes[i];
    const start = now + i * step;

    // Per-note volume envelope: near-instant attack, exponential ring-out.
    const voice = ctx.createGain();
    voice.gain.setValueAtTime(0.0001, start);
    voice.gain.exponentialRampToValueAtTime(CHIME_PEAK, start + 0.006);
    voice.gain.exponentialRampToValueAtTime(0.0001, start + ring);
    voice.connect(master);

    const fundamental = ctx.createOscillator();
    fundamental.type = "sine";
    fundamental.frequency.setValueAtTime(freq, start);
    fundamental.connect(voice);
    fundamental.start(start);
    fundamental.stop(start + ring + 0.02);

    // Octave overtone at ~28% for shimmer/bell character.
    const overtone = ctx.createOscillator();
    overtone.type = "sine";
    overtone.frequency.setValueAtTime(freq * 2, start);
    const overtoneGain = ctx.createGain();
    overtoneGain.gain.setValueAtTime(0.28, start);
    overtone.connect(overtoneGain);
    overtoneGain.connect(voice);
    overtone.start(start);
    overtone.stop(start + ring + 0.02);
  }
}

export function playInboxDing(): void {
  const ctx = ensureContext();
  if (!ctx) return;
  if (ctx.state === "running") {
    emitChime(ctx);
    return;
  }
  // Suspended/interrupted (autoplay policy or backgrounded tab): resume
  // first, then chime. `resume()` is async, so emitting here instead of
  // after a synchronous state check is what keeps the first ding from being
  // dropped.
  void ctx
    .resume()
    .then(() => {
      if (ctx.state === "running") emitChime(ctx);
    })
    .catch(() => {});
}
