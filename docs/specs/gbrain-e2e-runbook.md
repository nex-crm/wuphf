# gbrain Knowledge backend — manual e2e runbook

Run the full compounding loop yourself: **office chat → captured into gbrain
(with provenance + associations) → retrieved into a new chat as context with
zero prompting.** Everything below works **keyless** via local Ollama
embeddings.

## 0. Prereqs (one-time)

```bash
# gbrain CLI (npm) + Ollama with an embedding model — no API key needed.
npm i -g gbrain
brew install ollama && ollama serve &        # if not already running
ollama pull nomic-embed-text

# Build the WUPHF binary from this branch.
go build -o /tmp/wuphf ./cmd/wuphf
```

Verify: `gbrain --version` (≥ 0.42) and `ollama list` shows `nomic-embed-text`.

---

## 1. See the WHOLE cycle in one command (fastest)

This drives the real production code paths (capture writer + the exact
`FetchBrief` call `headless_claude` injects every turn) against a real gbrain.

```bash
export HOME=/tmp/gbrain-e2e            # isolated brain; won't touch your real one
rm -rf "$HOME" && mkdir -p "$HOME"
gbrain init --pglite --embedding-model ollama:nomic-embed-text

WUPHF_GBRAIN_IT=1 HOME=/tmp/gbrain-e2e \
  go test ./internal/team/ -run TestGBrainCompoundingCycle -count=1 -v
```

In the `-v` output you will see, in order:
- the **captured page** with the provenance line `> Captured from WUPHF office · chat · …` and `tags:[chat office]` + frontmatter `{kind, origin, source:wuphf-office}`,
- the **auto-association** `[{To:pricing-strategy … Source:wuphf-capture}]` created on capture,
- the **`== GBRAIN CONTEXT ==` brief** that a *new* differently-worded chat message pulls back automatically.

`--- PASS` means the loop is intact.

---

## 2. Inspect the brain by hand (see the artifacts gbrain stored)

Using the same `HOME=/tmp/gbrain-e2e` brain from step 1:

```bash
export HOME=/tmp/gbrain-e2e

# Capture a fresh "office decision" yourself (this is what put_page receives):
gbrain put decisions/launch-date <<'MD'
---
title: Launch date
tags: [office, decision]
---
> Captured from WUPHF office · decision · launch · 2026-06-27

We decided to launch the Pro plan on Friday. Pricing is $49/mo.
MD

gbrain list                                   # the page is there
gbrain get decisions/launch-date              # provenance line is in the body
gbrain query "when are we launching and at what price?"   # semantic retrieval
gbrain call get_links '{"slug":"chat-pricing-2026-06-26"}'  # associations (step 1's page)
```

`query` returns the right page even though your wording differs from the text —
that is the Ollama embeddings doing semantic retrieval.

---

## 3. See a gbrain page render in the browser

The reader renders gbrain pages through the unchanged `/wiki/*` shapes. Capture
a screenshot via the mocked-API harness (no broker needed):

```bash
cd web
bun run dev --port 5291 --strictPort &       # fresh vite from THIS worktree
cd ..
BASE_URL=http://localhost:5291 WUPHF_SCREENSHOTS_OUT=/tmp/shots \
  node web/e2e/screenshots/gbrain-wiki.mjs
open /tmp/shots/01-gbrain-wiki-page.png       # provenance quote, mermaid, TOC, cross-ref
```

(Stop vite with `pkill -f 'vite --port 5291'` when done.)

---

## 4. Full live office (advanced — real agents, real chat)

This boots the whole app with gbrain as the Knowledge backend and a real LLM
provider (your `claude` / `codex` login).

> **Caveat:** a second WUPHF instance refuses to start while another broker is
> on the default ports 7890/7891 (a workspace-migration safety guard). Stop any
> running WUPHF first, or run this as your primary instance.

```bash
# Reuse the keyed brain from step 1 (HOME must point at it so `gbrain serve`,
# which the broker spawns, finds the brain).
HOME=/tmp/gbrain-e2e WUPHF_MEMORY_BACKEND=gbrain \
  WUPHF_CHAT_DIGEST_INTERVAL=2m \
  /tmp/wuphf --no-open
```

Then, in the office UI:
1. Have a short threaded conversation in a channel that reaches a decision.
2. Within ~2 minutes the chat-digest sweep captures it into gbrain
   (`gbrain list` shows a new `chat-…` page; `get_links` shows its associations).
3. Open a **new** chat and ask a related question in different words — the agent
   answers using the captured knowledge **without you pasting any context**
   (it arrives as the `== GBRAIN CONTEXT ==` block in the agent's turn).

Tip: `WUPHF_MEMORY_BACKEND` defaults to gbrain automatically when gbrain is
installed and an embedder (OpenAI key **or** local Ollama) is available — the
explicit flag above just makes the intent obvious.

---

## Cleanup

```bash
chmod -R u+w /tmp/gbrain-e2e && rm -rf /tmp/gbrain-e2e /tmp/shots
pkill -f 'vite --port 5291'; pkill -f '/tmp/wuphf'
```
