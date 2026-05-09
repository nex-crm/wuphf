# `@wuphf/protocol` — manually-runnable demos

The package is pure types + helpers, but the moat invariants are easier to trust
when you watch them fire on adversarial inputs than when you read fast-check
arbitraries. Two demo artifacts ship alongside the test suite:

## `demo.ts` — moat behavior walkthrough

```bash
bun run packages/protocol/scripts/demo.ts
```

Eleven scenarios, ~25 assertions. Each scenario prints what we tried, what the
moat must do, and what it actually did. Sample output:

```text
── Scenario 3: SanitizedString rejects untrusted graph BEFORE side-effects fire
  PASS threw: SanitizedString: accessor property at $.tricky
  PASS getter side-effect did NOT fire = false
  PASS threw: SanitizedString: toJSON method at $ is not allowed
```

Use this as the human-eyeball companion to `bash scripts/test-protocol.sh`.
Adding new scenarios: copy an existing block in `demo.ts`, replace inputs and
expectations. The `expectThrows` / `expectEqual` / `expectChainResult` helpers
print a colored PASS/FAIL line and update the running tally.

## `../testdata/verifier-reference.go` — cross-language wire-contract proof

```bash
cd packages/protocol/testdata
go run verifier-reference.go
```

Stdlib-only Go program that loads `audit-event-vectors.json`, recomputes the
canonical serialization and eventHash for each vector, and asserts byte-equality
with the bundled `expected.*` values. If it prints `All N vectors match`, the
TypeScript writer and an independent Go reader agree on the wire bytes. If it
fails, the wire contract has drifted — coordinate the bump with downstream
consumers before landing.

This is the single highest-leverage piece of evidence that the audit chain is
cross-language portable. Run it after any change to `audit-event.ts`,
`canonical-json.ts`, or the testdata fixtures.
