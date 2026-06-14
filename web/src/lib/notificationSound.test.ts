import { afterEach, describe, expect, it, vi } from "vitest";

// Minimal Web Audio fakes — happy-dom ships no AudioContext, and we only
// need to observe that the chime schedules oscillators and resumes a
// suspended context before playing.
class FakeAudioParam {
  setValueAtTime = vi.fn(() => this);
  linearRampToValueAtTime = vi.fn(() => this);
  exponentialRampToValueAtTime = vi.fn(() => this);
}

class FakeGainNode {
  gain = new FakeAudioParam();
  connect = vi.fn();
}

class FakeOscillator {
  type = "sine";
  frequency = new FakeAudioParam();
  connect = vi.fn();
  start = vi.fn();
  stop = vi.fn();
}

// The module constructs `new AudioContext()` with no args, so the desired
// initial state and a handle to the created instance are threaded through
// these module-scoped variables instead of constructor params.
let initialState: "running" | "suspended" = "running";
let createdContext: FakeAudioContext | null = null;

class FakeAudioContext {
  state: "running" | "suspended" = initialState;
  currentTime = 0;
  destination = {};
  oscillators: FakeOscillator[] = [];
  // Real AudioContext.resume() is async: the state stays "suspended" until
  // the promise resolves. Modelling that is what makes the dropped-first-ding
  // test bite — code that checks state synchronously after resume() sees
  // "suspended" and (pre-fix) bailed without playing.
  resume = vi.fn(() =>
    Promise.resolve().then(() => {
      this.state = "running";
    }),
  );
  constructor() {
    createdContext = this;
  }
  createGain(): FakeGainNode {
    return new FakeGainNode();
  }
  createOscillator(): FakeOscillator {
    const osc = new FakeOscillator();
    this.oscillators.push(osc);
    return osc;
  }
}

function installContext(state: "running" | "suspended" | null): void {
  const w = window as unknown as {
    AudioContext?: unknown;
    webkitAudioContext?: unknown;
  };
  createdContext = null;
  if (state === null) {
    w.AudioContext = undefined;
  } else {
    initialState = state;
    w.AudioContext = FakeAudioContext;
  }
  w.webkitAudioContext = undefined;
}

async function loadModule() {
  // notificationSound caches its AudioContext at module scope; reset so each
  // test gets a fresh one bound to the context we installed.
  vi.resetModules();
  return import("./notificationSound");
}

afterEach(() => {
  installContext(null);
});

describe("playInboxDing", () => {
  it("uses a loud peak amplitude (guards against a silent regression)", async () => {
    installContext("running");
    const { CHIME_PEAK } = await loadModule();
    expect(CHIME_PEAK).toBeGreaterThanOrEqual(0.25);
  });

  it("schedules oscillators immediately when the context is running", async () => {
    installContext("running");
    const { playInboxDing } = await loadModule();

    playInboxDing();

    // Multi-note chime — several oscillators are scheduled synchronously.
    expect(createdContext?.oscillators.length ?? 0).toBeGreaterThan(0);
  });

  it("resumes a suspended context and still plays (no dropped first ding)", async () => {
    installContext("suspended");
    const { playInboxDing } = await loadModule();

    playInboxDing();
    const ctx = createdContext;
    expect(ctx?.resume).toHaveBeenCalled();
    // Synchronously the context is still suspended — nothing has played yet
    // (and pre-fix, nothing ever would).
    expect(ctx?.oscillators.length).toBe(0);
    // Flush resume()'s microtasks; the chime fires once the context runs.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(ctx?.oscillators.length ?? 0).toBeGreaterThan(0);
  });

  it("no-ops without throwing when Web Audio is unavailable", async () => {
    installContext(null);
    const { playInboxDing } = await loadModule();
    expect(() => playInboxDing()).not.toThrow();
  });
});
