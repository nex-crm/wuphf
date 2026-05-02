# Wiki Compress — ICP Tutorial Examples

These three concrete personas drive the spec for PR 4 (article compression).
The feature must work end-to-end for all three before the PR is ready.

The endpoint: `POST /wiki/compress?path=<relPath>` accepts an article path,
asks the LLM to rewrite the body shorter (40 to 60 percent of original word
count), preserves frontmatter and named facts, and commits the result under
the synthetic `archivist` git identity. The response is a small JSON
envelope so callers can react to either the queued case or the in-flight
debounce case.

Response shape:

```json
{
  "queued": true,
  "in_flight": false,
  "path": "team/people/old-contact.md"
}
```

---

## Example 1 — Alex, Account Executive

**Persona.** Alex is an AE who keeps a wiki of customer briefs that have
grown verbose after months of fact-log appends and ad-hoc edits.

**Input state.** Article `team/customers/contoso.md` is roughly 800 words
of prose with two synthesis passes worth of redundancy and three
duplicated paragraphs about the renewal contact.

**Action.** Alex opens the article in the wiki UI. The page header shows
a `Compress` button (visible because `word_count > 200`). He clicks it.

**Expected output.**

- The browser POSTs to `/wiki/compress?path=team/customers/contoso.md`.
- The response is `{queued: true, in_flight: false, path: "team/customers/contoso.md"}`.
- A toast shows `Compressing article…`.
- About 10 seconds later the archivist commits a rewritten body roughly
  40 to 60 percent of the original word count, all named facts and the
  renewal contact preserved, original frontmatter intact.
- The next page load (or refresh after the worker completes) shows the
  shorter article.

---

## Example 2 — Jordan, CS Manager

**Persona.** Jordan is a CS manager triggering compress in a meeting.

**Input state.** Compress was just clicked on
`team/customers/globex.md`. The first job is mid-flight inside the
WikiCompressor goroutine.

**Action.** Jordan double-clicks the `Compress` button or refreshes and
clicks again before the first compress finishes.

**Expected output.**

- The second `POST /wiki/compress?path=team/customers/globex.md` returns
  `{queued: false, in_flight: true, path: "team/customers/globex.md"}`.
- The toast text changes to `Already compressing, check back soon.`
- No second LLM call is made. No second commit lands. The first compress
  completes normally.

---

## Example 3 — Marcus, RevOps Lead

**Persona.** Marcus runs ops scripts against the broker over its HTTP
surface (functionally the MCP path: same endpoints, agent caller).

**Input state.** Marcus has a stale contact brief
`team/people/old-contact.md` that is 600 words of accumulated history
and wants the agent to compact it.

**Action.** Marcus calls
`POST /wiki/compress?path=team/people/old-contact.md` from his agent.

**Expected output.**

- Response is `{queued: true, in_flight: false, path: "team/people/old-contact.md"}`
  with HTTP 200.
- The compressor goroutine picks up the job, calls the LLM with the
  `CompressPromptSystem` system prompt, validates the output, re-applies
  the original frontmatter, and commits via the wiki worker queue under
  the archivist identity. Commit message: `archivist: compress team/people/old-contact.md`.
- Marcus's agent polls `GET /wiki/article?path=team/people/old-contact.md`
  and detects `word_count` has dropped from roughly 600 to roughly 300.
