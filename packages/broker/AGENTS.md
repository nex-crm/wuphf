# @wuphf/broker — Agent Guidelines

Branch-4 slice: the loopback listener that everything later in the rewrite
plugs into. The public surface is `createBroker(config) → BrokerHandle`.

If a rule below conflicts with what your prompt asked you to do, stop and
surface the conflict. The user picks; do not silently choose.

## Hard rules

1. **Bind to `127.0.0.1` only.** No flag, env var, or config knob may flip
   this to `0.0.0.0`, a LAN address, or a remote host. The package owns the
   loopback contract; widening it is a security regression.
2. **The DNS-rebinding guard runs before every route handler.** Both checks
   must pass: `Host` is one of the allowed loopback hostnames AND `RemoteAddr`
   is a loopback peer IP. Either alone is insufficient (a misbound listener
   defeats the host check; a rebound DNS name defeats the peer check).
3. **Bearer comparison is constant-time.** Always use
   `node:crypto.timingSafeEqual` (via `tokenMatches`) — never `===`.
4. **`/api-token` is the only bootstrap route without a bearer.** Every other
   `/api/*` and `/terminal/*` surface requires the token. `/`, `/index.html`,
   and `/assets/*` are the static surfaces and are loopback-guarded but do not
   require a bearer (the renderer fetches the bundle before it knows the token).
5. **No `contextBridge`-style channels in this package.** This package is
   pure Node and must not depend on `electron`. App data flows over the
   listener; OS verbs are the desktop shell's concern.
6. **Do not import private files of `@wuphf/protocol`.** Always import from
   the package root (`@wuphf/protocol`). The protocol package's public surface
   is the wire contract; reaching into `src/<sub>.ts` couples this package to
   internal layout.
7. **Wire shape is the `@wuphf/protocol` codec output.** `/api-token` returns
   `apiBootstrapToJson(...)` exactly. Hand-rolling the JSON would let this
   package and the renderer drift from the protocol package's wire-shape
   guarantees.
8. **No `Date.now()` for ordering or IDs.** Per the protocol-package
   discipline (`packages/protocol/AGENTS.md` rule 14): `Date` may mark time
   (e.g. `emittedAt` on the `ready` SSE event), but never order or
   deduplicate. Use explicit counters / ULIDs / `EventLsn` for ordering.
9. **Do not introduce a per-route static-content cache.** Branch-4 emits
   `Cache-Control: no-store` everywhere. The renderer bundle is small and the
   listener is loopback; cache headers add complexity without measurable
   benefit and can hide bundle-version bugs.

## Adding a route

Branch-4 ships only the routes listed in `README.md`. Adding a new one
requires:

- A handler in `src/listener.ts` (or a new module under `src/`) gated by
  `checkLoopbackRequest` and (for app data) `authorize`.
- Tests that cover the loopback gate, the auth gate, and the success path.
- A wire-shape decision: if the new endpoint emits a payload that travels
  to the renderer, the shape MUST go through a `@wuphf/protocol` codec —
  no per-route ad-hoc JSON.
- A row in `README.md`'s route table and a paragraph in
  `docs/modules/listener.md` describing the contract.

## Testing

- `tests/*.spec.ts` runs via Vitest with `environment: "node"`.
- Bring the listener up on `port: 0`; capture `broker.url` for `fetch` calls.
- Always `await broker.stop()` in the test's `afterEach` (or in a
  `try/finally`) so a failing assertion does not leak a listening socket.
- Tests do not need `electron` and must not import it. The package is pure
  Node; tests are too.

## Validation

```bash
bun run typecheck
bun run test
bun run check
bun run check:invariants
```
