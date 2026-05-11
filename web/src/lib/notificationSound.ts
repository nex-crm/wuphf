/**
 * Two-tone notification ding for inbox count increases. Web Audio API
 * synthesis — no asset, no MIME, no decoding. Polite volume default
 * (0.05 gain). Lazy AudioContext init; no-ops cleanly when the browser
 * has not yet received a user gesture (autoplay policy).
 */

let cachedContext: AudioContext | null = null;

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

export function playInboxDing(): void {
  const ctx = ensureContext();
  if (!ctx) return;
  if (ctx.state === "suspended") {
    void ctx.resume().catch(() => {});
  }
  if (ctx.state !== "running") return;

  const now = ctx.currentTime;
  const master = ctx.createGain();
  master.gain.setValueAtTime(0, now);
  master.gain.linearRampToValueAtTime(0.05, now + 0.02);
  master.gain.exponentialRampToValueAtTime(0.0001, now + 0.32);
  master.connect(ctx.destination);

  // Two-tone: 880 Hz (A5) then 1320 Hz (E6) — clean ascending step.
  const tone = (freq: number, start: number, duration: number) => {
    const osc = ctx.createOscillator();
    osc.type = "sine";
    osc.frequency.setValueAtTime(freq, now + start);
    osc.connect(master);
    osc.start(now + start);
    osc.stop(now + start + duration);
  };
  tone(880, 0, 0.18);
  tone(1320, 0.12, 0.2);
}
