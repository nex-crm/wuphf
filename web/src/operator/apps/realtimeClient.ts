// realtimeClient.ts — the real "Demo workflow to Nex" call: a screen-share +
// realtime-voice session against OpenAI's Realtime API over WebRTC.
//
// Flow (see docs/specs/operator-demo-call-real.md):
//   1. capture the operator's screen (getDisplayMedia) + mic (getUserMedia)
//   2. ask the broker to mint a short-lived EPHEMERAL key (the real key never
//      reaches the browser) — POST /realtime/session
//   3. WebRTC handshake with OpenAI using the ephemeral key (SDP offer/answer)
//   4. stream mic up, play model voice down, sample screen frames as vision
//   5. the model calls the draft_workflow tool → we surface the captured draft
//
// The exact Realtime event vocabulary has shifted across versions, so the event
// handling is deliberately tolerant: it keys off the event `type` substrings it
// recognizes and ignores the rest.

import { post } from "../../api/client";
import type { DraftWorkflowArgs } from "./demoCapture";

export interface RealtimeStatus {
  phase:
    | "requesting-screen"
    | "connecting"
    | "live"
    | "drafted"
    | "ended"
    | "error";
  detail?: string;
}

export interface RealtimeTranscriptLine {
  who: "you" | "ai";
  text: string;
  final: boolean;
}

export interface StartRealtimeOptions {
  mode: "build" | "modify";
  tool?: { id: string; name: string };
  onStatus: (s: RealtimeStatus) => void;
  onTranscript: (line: RealtimeTranscriptLine) => void;
  onDraft: (args: DraftWorkflowArgs) => void;
  onError: (message: string) => void;
  // True while Nex is processing a reply but has not started speaking yet, so
  // the UI can show (and sound) a "thinking" state instead of looking frozen.
  onThinking?: (thinking: boolean) => void;
  // Live mic + model speech levels (0..1), for the call avatars. Fires ~30/s.
  onLevels?: (you: number, ai: number) => void;
  // <audio> element the model's voice plays through.
  audioEl: HTMLAudioElement;
  // The preview <video> the modal renders the screen share into. We sample
  // frames from it for vision; without it the call still works, just blind.
  videoEl?: HTMLVideoElement;
}

export interface RealtimeController {
  stop: () => void;
  // The live screen-share stream, so the modal can show a preview.
  screenStream: MediaStream;
}

interface RealtimeSessionToken {
  ephemeral_key: string;
  model: string;
  sdp_url: string;
  expires_at?: number;
}

// Live call state shared between the data-channel event loop and the frame timer.
interface CallState {
  aiText: string;
  greeted: boolean;
  drafted: boolean;
  responding: boolean;
  userSpeaking: boolean;
  // True from when Nex starts generating a reply until its first spoken word.
  thinking: boolean;
}

// How often we push a screen frame into the conversation as vision input. Lower
// is more responsive to on-screen changes (less lag) at more vision token cost.
const FRAME_INTERVAL_MS = 1500;
// Every Nth frame, if Nex is idle and the operator is not speaking, invite it to
// note any NEW step it sees — so it narrates the demo even during silent clicks.
const NUDGE_EVERY_FRAMES = 5;
// Downscale frames to bound vision token cost.
const FRAME_MAX_WIDTH = 1024;

const DRAFT_TOOL = {
  type: "function" as const,
  name: "draft_workflow",
  description:
    "Call this ONLY after the operator has explicitly confirmed they are ready " +
    "for you to leave the call and build the app. It ends the call and starts " +
    "building the app from the demonstrated workflow, so never call it on a " +
    "pause or your own judgment. Capture everything you observed for the build.",
  parameters: {
    type: "object",
    properties: {
      goal: {
        type: "string",
        description:
          "One clean imperative sentence: the workflow to build, or the change to make.",
      },
      summary: {
        type: "string",
        description: "A short reflect-back of what you drafted.",
      },
      screens: {
        type: "array",
        description: "Screens/apps the operator demonstrated on.",
        items: {
          type: "object",
          properties: {
            label: { type: "string" },
            url: { type: "string" },
            dom: { type: "string" },
          },
          required: ["label"],
        },
      },
      selectors: {
        type: "array",
        description: "Concrete UI elements they interacted with.",
        items: {
          type: "object",
          properties: {
            label: { type: "string" },
            role: { type: "string" },
            selector: { type: "string" },
            sample: { type: "string" },
          },
          required: ["label", "selector"],
        },
      },
      apiCalls: {
        type: "array",
        description: "API calls/integrations observed (no secrets).",
        items: {
          type: "object",
          properties: {
            method: { type: "string" },
            endpoint: { type: "string" },
            integration: { type: "string" },
            purpose: { type: "string" },
          },
          required: ["endpoint"],
        },
      },
      entities: {
        type: "array",
        description: "Integrations, channels, thresholds, fields, actions.",
        items: {
          type: "object",
          properties: {
            kind: { type: "string" },
            value: { type: "string" },
          },
          required: ["value"],
        },
      },
    },
    required: ["goal"],
  },
} as const;

function instructionsFor(opts: StartRealtimeOptions): string {
  const base =
    "You are Nex. The operator demonstrates a WORKFLOW on their shared screen, and from that workflow you will build them an APP " +
    "(a small internal tool). Always say you are building the APP from the workflow they show you — never call it 'building the workflow'. " +
    "OPEN THE CALL by warmly greeting them in one short sentence and saying you are ready to watch them work and learn their workflow — for example: " +
    "\"Hey, I'm ready when you are. Walk me through what you do, and I'll watch your screen and learn it, then build you an app from it.\" " +
    "\n\nAs they work, WATCH THE SCREEN CLOSELY and NARRATE WHAT YOU SEE. When a meaningful step happens — they open an app, search a record, " +
    "copy a value, click a button, send a message — briefly confirm it out loud in one short sentence so they know you caught it " +
    '(e.g., "Got it, you looked up the company in HubSpot," or "Okay, you posted that to #ae-handoffs"). Keep confirmations short and ' +
    "natural, not a play-by-play of every pixel.\n\n" +
    "BE GENUINELY CURIOUS. Ask sharp questions to capture what the app will need when you build it: what triggers this? what decides one " +
    "path versus another (thresholds, fields, conditions)? what happens to the cases that do not qualify? which app or channel does each " +
    "step touch, and what gets written where? what should happen when something is missing or ambiguous? Ask one question at a time and " +
    "keep it conversational.\n\n" +
    "Track everything you learn: the trigger, each app/integration and the API calls you can see, the decision logic and its branches, the " +
    "actions and where they write.\n\n" +
    "NEVER END THE CALL ON YOUR OWN, AND NEVER DECIDE BY YOURSELF THAT YOU ARE DONE. When you think you have the whole flow, or the operator " +
    "pauses, ASK FOR EXPLICIT CONFIRMATION before wrapping up — for example: \"I think I've got the whole flow. Are you ready for me to leave " +
    'the call and build the app from this, or is there more you want to show me?" Then WAIT and listen. ' +
    'ONLY when the operator gives a CLEAR yes to building it now (e.g., "yes", "build it", "go ahead", "that\'s it, build the app") do you ' +
    'call the draft_workflow tool with everything you captured, and say one short line like "Perfect — leaving the call and building your app now." ' +
    "Calling draft_workflow immediately ENDS the call and starts building the app — it is a one-way door. Do NOT call it on a pause, an ambiguous " +
    "comment, silence, or your own judgment that the workflow looks complete. If they say no, not yet, or keep going, stay on the call and keep learning.";
  if (opts.mode === "modify" && opts.tool) {
    return `${base} The operator is CHANGING an existing app named "${opts.tool.name}". Open by saying you're ready to see the change, narrate the change as they show it, ask what should stay the same, and the same rule applies: only call draft_workflow after they explicitly confirm they're ready for you to leave the call and update the app.`;
  }
  return base;
}

export async function startRealtimeCall(
  opts: StartRealtimeOptions,
): Promise<RealtimeController> {
  opts.onStatus({ phase: "requesting-screen" });

  // 1. Screen + mic. getDisplayMedia throws if the user cancels the picker.
  const screenStream = await navigator.mediaDevices.getDisplayMedia({
    video: { frameRate: 5 },
    audio: false,
  });
  let micStream: MediaStream;
  try {
    micStream = await navigator.mediaDevices.getUserMedia({ audio: true });
  } catch (err) {
    stopStream(screenStream);
    throw new Error(
      "Microphone access is required for the voice call. " + errMessage(err),
    );
  }

  opts.onStatus({ phase: "connecting" });

  // 2. Mint the ephemeral key server-side (real key stays on the broker).
  let token: RealtimeSessionToken;
  try {
    token = await post<RealtimeSessionToken>("/realtime/session", {});
  } catch (err) {
    stopStream(screenStream);
    stopStream(micStream);
    throw new Error(
      "Could not start a realtime session. Add your OpenAI Realtime key in Settings. " +
        errMessage(err),
    );
  }

  // 3. WebRTC peer connection.
  const pc = new RTCPeerConnection();
  // Audio level metering for the call avatars. The mic analyser is set up now;
  // the model-voice analyser is wired when its track arrives.
  const meter = createLevelMeter(micStream, opts.onLevels);
  pc.ontrack = (e) => {
    const remote = e.streams[0] ?? new MediaStream([e.track]);
    opts.audioEl.srcObject = remote;
    meter.attachRemote(remote);
  };
  for (const track of micStream.getAudioTracks()) {
    pc.addTrack(track, micStream);
  }

  // Shared event-loop state: the AI transcript buffer, a one-shot greeting guard
  // (Nex opens the call once, after the session is configured), a one-shot draft
  // guard (draft_workflow surfaced once), and live turn flags so the observation
  // nudge never fires while Nex is already talking or the operator is speaking.
  const state: CallState = {
    aiText: "",
    greeted: false,
    drafted: false,
    responding: false,
    userSpeaking: false,
    thinking: false,
  };
  const dc = pc.createDataChannel("oai-events");
  dc.onopen = () => {
    // GA session schema: session.type is REQUIRED, audio config is nested, and
    // turn_detection (server VAD) is what makes the model reply when you speak.
    dc.send(
      JSON.stringify({
        type: "session.update",
        session: {
          type: "realtime",
          instructions: instructionsFor(opts),
          output_modalities: ["audio"],
          audio: {
            input: {
              // semantic_vad reads end-of-turn from meaning, so it does not cut
              // the operator off mid-sentence while they pause to click around —
              // the right fit for narrating a demo (OpenAI's recommended mode).
              // High eagerness keeps Nex's replies snappy rather than laggy.
              turn_detection: { type: "semantic_vad", eagerness: "high" },
              transcription: { model: "gpt-4o-mini-transcribe" },
            },
            output: { voice: "marin" },
          },
          tools: [DRAFT_TOOL],
          tool_choice: "auto",
        },
      }),
    );
  };
  dc.onmessage = (e) =>
    handleEvent(e.data, state, dc, opts).catch((err) =>
      opts.onError(errMessage(err)),
    );

  // 4. SDP offer/answer with OpenAI, authorized by the ephemeral key only. The
  // model is carried by the ephemeral token, so no query param is needed.
  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  const sdpResp = await fetch(token.sdp_url, {
    method: "POST",
    body: offer.sdp,
    headers: {
      Authorization: `Bearer ${token.ephemeral_key}`,
      "Content-Type": "application/sdp",
    },
  });
  if (!sdpResp.ok) {
    cleanup();
    throw new Error(`Realtime handshake failed (${sdpResp.status}).`);
  }
  await pc.setRemoteDescription({ type: "answer", sdp: await sdpResp.text() });

  opts.onStatus({ phase: "live" });

  // 5. Vision: push a downscaled screen frame on an interval so Nex sees what is
  // happening with little lag. Periodically, when Nex is idle and the operator
  // is not speaking, invite it to note a new step — so it narrates silent clicks
  // without ever talking over anyone.
  let frameTick = 0;
  const frameTimer = window.setInterval(() => {
    const frame = opts.videoEl ? grabFrame(opts.videoEl) : null;
    if (!frame || dc.readyState !== "open") return;
    dc.send(
      JSON.stringify({
        type: "conversation.item.create",
        item: {
          type: "message",
          role: "user",
          content: [{ type: "input_image", image_url: frame }],
        },
      }),
    );
    frameTick++;
    if (
      frameTick % NUDGE_EVERY_FRAMES === 0 &&
      state.greeted &&
      !state.responding &&
      !state.userSpeaking &&
      !state.drafted
    ) {
      state.responding = true;
      dc.send(
        JSON.stringify({
          type: "response.create",
          response: {
            instructions:
              "If you see a NEW meaningful step on screen since you last spoke, confirm it in one short sentence and, if helpful, ask one curious question about it. If nothing new is happening, stay silent and do not speak.",
          },
        }),
      );
    }
  }, FRAME_INTERVAL_MS);

  // If the operator stops sharing from the browser chrome, end the call.
  for (const track of screenStream.getVideoTracks()) {
    track.addEventListener("ended", () => cleanup("ended"));
  }

  function cleanup(phase: RealtimeStatus["phase"] = "ended") {
    window.clearInterval(frameTimer);
    meter.stop();
    try {
      dc.close();
    } catch {
      /* already closed */
    }
    try {
      pc.close();
    } catch {
      /* already closed */
    }
    stopStream(screenStream);
    stopStream(micStream);
    opts.audioEl.srcObject = null;
    opts.onStatus({ phase });
  }

  return { stop: () => cleanup("ended"), screenStream };
}

async function handleEvent(
  raw: unknown,
  state: CallState,
  dc: RTCDataChannel,
  opts: StartRealtimeOptions,
): Promise<void> {
  if (typeof raw !== "string") return;
  let ev: Record<string, unknown>;
  try {
    ev = JSON.parse(raw);
  } catch {
    return;
  }
  const type = typeof ev.type === "string" ? ev.type : "";

  // Track who holds the turn so the observation nudge never talks over anyone,
  // and surface a "thinking" state from when Nex starts generating a reply until
  // its first spoken word, so the UI never looks frozen.
  if (type === "response.created") {
    state.responding = true;
    setThinking(state, opts, true);
  }
  if (type === "response.done") {
    state.responding = false;
    setThinking(state, opts, false);
  }
  if (type === "input_audio_buffer.speech_started") {
    state.userSpeaking = true;
    setThinking(state, opts, false);
  }
  if (type === "input_audio_buffer.speech_stopped") state.userSpeaking = false;
  // Nex has started speaking — stop "thinking".
  if (
    type.includes("output_audio.delta") ||
    type.includes("audio_transcript.delta")
  ) {
    setThinking(state, opts, false);
  }

  // Once the session is configured (instructions + VAD live), have Nex open the
  // call exactly once. Greeting before this would use default behavior.
  if (type === "session.updated" && !state.greeted) {
    state.greeted = true;
    state.responding = true;
    dc.send(JSON.stringify({ type: "response.create" }));
    return;
  }

  // Model speech transcript (partial → final).
  if (
    type.includes("audio_transcript.delta") ||
    type === "response.output_text.delta"
  ) {
    const delta = typeof ev.delta === "string" ? ev.delta : "";
    state.aiText += delta;
    opts.onTranscript({ who: "ai", text: state.aiText, final: false });
    return;
  }
  if (
    type.includes("audio_transcript.done") ||
    type === "response.output_text.done"
  ) {
    const text =
      typeof ev.transcript === "string"
        ? ev.transcript
        : typeof ev.text === "string"
          ? ev.text
          : state.aiText;
    state.aiText = "";
    if (text.trim()) opts.onTranscript({ who: "ai", text, final: true });
    return;
  }

  // The operator's own speech, transcribed.
  if (type === "conversation.item.input_audio_transcription.completed") {
    const text = typeof ev.transcript === "string" ? ev.transcript : "";
    if (text.trim()) opts.onTranscript({ who: "you", text, final: true });
    return;
  }

  // The draft_workflow tool call — the end of the demo. The canonical signal is
  // response.done carrying a function_call output item (with call_id); the
  // streaming response.function_call_arguments.done event is the fallback.
  const fc = extractDraftCall(type, ev);
  if (fc && !state.drafted) {
    state.drafted = true;
    try {
      const args = JSON.parse(fc.arguments || "{}") as DraftWorkflowArgs;
      // Acknowledge the tool so the model can give a short closing line instead
      // of hanging, then surface the draft to the UI.
      if (fc.callId) {
        dc.send(
          JSON.stringify({
            type: "conversation.item.create",
            item: {
              type: "function_call_output",
              call_id: fc.callId,
              output: JSON.stringify({ status: "drafted" }),
            },
          }),
        );
        state.responding = true;
        dc.send(JSON.stringify({ type: "response.create" }));
      }
      opts.onStatus({ phase: "drafted" });
      opts.onDraft(args);
    } catch (err) {
      state.drafted = false;
      opts.onError(`Could not read the drafted workflow. ${errMessage(err)}`);
    }
    return;
  }

  if (type === "error") {
    const msg =
      ev.error && typeof ev.error === "object" && "message" in ev.error
        ? String((ev.error as { message: unknown }).message)
        : "Realtime error";
    opts.onError(msg);
  }
}

// Flip the "thinking" flag at most once per transition and notify the UI.
function setThinking(
  state: CallState,
  opts: StartRealtimeOptions,
  on: boolean,
): void {
  if (state.thinking === on) return;
  state.thinking = on;
  opts.onThinking?.(on);
}

interface DraftCall {
  callId: string;
  arguments: string;
}

// Pull a draft_workflow function call out of whichever event carries it:
// response.done (canonical, with the output item + call_id) or the streaming
// function_call_arguments.done fallback.
function extractDraftCall(
  type: string,
  ev: Record<string, unknown>,
): DraftCall | null {
  if (type === "response.done") {
    const response = ev.response as { output?: unknown } | undefined;
    const output = Array.isArray(response?.output) ? response.output : [];
    for (const item of output) {
      const o = item as Record<string, unknown>;
      if (o.type === "function_call" && o.name === "draft_workflow") {
        return {
          callId: typeof o.call_id === "string" ? o.call_id : "",
          arguments: typeof o.arguments === "string" ? o.arguments : "{}",
        };
      }
    }
    return null;
  }
  if (
    type === "response.function_call_arguments.done" &&
    ev.name === "draft_workflow"
  ) {
    return {
      callId: typeof ev.call_id === "string" ? ev.call_id : "",
      arguments: typeof ev.arguments === "string" ? ev.arguments : "{}",
    };
  }
  return null;
}

interface LevelMeter {
  attachRemote: (stream: MediaStream) => void;
  stop: () => void;
}

// Web Audio level metering for the call avatars: RMS amplitude (0..1) of the mic
// ("you") and the model's voice ("ai"), pushed ~per animation frame. A no-op
// when no consumer is interested.
function createLevelMeter(
  micStream: MediaStream,
  onLevels?: (you: number, ai: number) => void,
): LevelMeter {
  if (!onLevels) return { attachRemote: () => {}, stop: () => {} };

  const ctx = new AudioContext();
  ctx.resume().catch(() => {
    /* best effort; the click gesture usually resumes it */
  });
  const youAnalyser = makeAnalyser(ctx, micStream);
  let aiAnalyser: AnalyserNode | null = null;
  let raf = 0;
  let stopped = false;

  const tick = () => {
    if (stopped) return;
    onLevels(rms(youAnalyser), aiAnalyser ? rms(aiAnalyser) : 0);
    raf = requestAnimationFrame(tick);
  };
  raf = requestAnimationFrame(tick);

  return {
    attachRemote(stream) {
      try {
        aiAnalyser = makeAnalyser(ctx, stream);
      } catch {
        /* remote not analyzable; avatar just stays calm */
      }
    },
    stop() {
      stopped = true;
      cancelAnimationFrame(raf);
      ctx.close().catch(() => {});
    },
  };
}

function makeAnalyser(ctx: AudioContext, stream: MediaStream): AnalyserNode {
  const src = ctx.createMediaStreamSource(stream);
  const analyser = ctx.createAnalyser();
  analyser.fftSize = 256;
  src.connect(analyser);
  return analyser;
}

// Root-mean-square amplitude of the analyser's current frame, scaled into a
// lively 0..1 range for the avatar glow.
function rms(analyser: AnalyserNode): number {
  const buf = new Uint8Array(analyser.frequencyBinCount);
  analyser.getByteTimeDomainData(buf);
  let sum = 0;
  for (let i = 0; i < buf.length; i++) {
    const v = (buf[i] - 128) / 128;
    sum += v * v;
  }
  return Math.min(1, Math.sqrt(sum / buf.length) * 3.2);
}

// Grab one frame from the live preview <video>, downscaled, as a JPEG data URL.
function grabFrame(video: HTMLVideoElement): string | null {
  // readyState < 2 means no current frame yet — skip this tick.
  if (video.readyState < 2 || !video.videoWidth) return null;
  const scale = Math.min(1, FRAME_MAX_WIDTH / video.videoWidth);
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(video.videoWidth * scale);
  canvas.height = Math.round(video.videoHeight * scale);
  const ctx = canvas.getContext("2d");
  if (!ctx) return null;
  try {
    ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
    return canvas.toDataURL("image/jpeg", 0.6);
  } catch {
    return null;
  }
}

function stopStream(stream: MediaStream): void {
  for (const track of stream.getTracks()) track.stop();
}

function errMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
