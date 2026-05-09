# Protocol Lessons Learned

These are the wire-shape failure modes that surfaced across the PR 743 review
rounds. Keep the examples small; the point is to make the review discipline
easy to recognize before repeating the bug.

1. **README drift**

   Failure mode: the wire shape changed, but the consumer-facing README still
   described the old contract.

   ```ts
   const bytes = concatAsciiLowerHex(prevHash, serializeAuditEventRecordForHash(record));
   const eventHash = sha256Hex(bytes);
   // README still says raw prevHash bytes are mixed instead of ASCII hex.
   ```

   Discipline: Hard rule #3 requires README updates with hash-chain changes,
   and any new README `wire shape:` literal must reference a typed test.

2. **Codec <-> validator literal duplication**

   Failure mode: codec and validator each carried their own `*_VALUES` tuple,
   so one copy drifted while the other still typechecked.

   ```ts
   const WRITE_RESULT_VALUES = ["applied", "rejected"] as const satisfies readonly WriteResult[];
   const WRITE_RESULT_VALUES = ["applied", "rejected", "pending"] as const;
   ```

   Discipline: Hard rules #1 and #8 require codec and validator branches to
   stay in lockstep; import shared literals from `src/receipt-literals.ts`.

3. **Golden vector incompleteness**

   Failure mode: tests pinned serialization bytes but not the derived
   `eventHash`, leaving half of the wire contract unguarded.

   ```ts
   expect(serializeAuditEventRecordForHash(record)).toEqual(expected.bytes);
   // Missing:
   expect(computeAuditEventHash(record)).toBe(expected.eventHash);
   ```

   Discipline: Hard rule #2 requires every wire-contract function to have a
   golden literal in `tests/audit-event.spec.ts`.

4. **`instanceof` trust**

   Failure mode: a forged object can pass `instanceof FrozenArgs` if the
   validator does not re-derive the canonical fields.

   ```ts
   const forged = Object.create(FrozenArgs.prototype) as FrozenArgs;
   Object.assign(forged, { canonicalJson: '{"a":1}', hash: wrongHash });
   expect(forged instanceof FrozenArgs).toBe(true);
   ```

   Discipline: Hard rule #4 requires validators to recompute canonical JSON or
   sanitized projections and compare bytes, not trust prototypes.

5. **Cross-field invariants enforced at one site, not all**

   Failure mode: one validator checked the approval-token hash binding while a
   sibling validator only checked `receiptId`.

   ```ts
   validateExternalWrite(token, receipt.id, proposedDiff.hash); // both bindings
   validateApprovalEvent(token, receipt.id); // receiptId only
   ```

   Discipline: Hard rule #5 requires every new sibling-with-`FrozenArgs` token
   site to add the hash binding where the diff is present.

6. **Demo as public-API smoke test**

   Failure mode: an `index.ts` export drift surfaced only when downstream
   consumers tried to import the package.

   ```ts
   import { receiptFromJson } from "@wuphf/protocol";

   receiptFromJson(sampleReceiptJson); // catches missing or renamed export
   ```

   Discipline: Hard rule #11 keeps public API changes explicit in `index.ts`,
   and demo-driven iteration catches package-surface drift before consumers do.

7. **Cross-language oracle**

   Failure mode: TypeScript serializer and in-package golden vectors can be
   updated together, hiding drift from external readers.

   ```ts
   const bytes = serializeAuditEventRecordForHash(record);
   const eventHash = computeAuditEventHash(record);
   // TS tests pass, but the Go verifier still encodes the old wire contract.
   ```

   Discipline: Hard rule #2 requires golden vectors, and audit-chain changes
   must also run the independent Go reference verifier.

8. **Sub-agent prompt completeness**

   Failure mode: delegated agents needed a second pass when the prompt omitted
   the AGENTS path, ambiguity options, verification commands, or disposition
   format.

   ```text
   Prompt: "Fix the review comments."
   Result: "done"
   Missing: FIXED / SKIPPED+reason / DEFERRED+issue and exact checks run.
   ```

   Discipline: Hard rule #12 requires self-contained sub-agent prompts with
   hard rules quoted, checks named, scope bounded, and dispositions specified.

9. **Sustainability dimension**

   Failure mode: a verifier or recovery path that works on a tiny fixture can
   still be unsafe when it materializes unbounded protocol data.

   ```ts
   const events = await readAllAuditEvents(path);
   verifyAuditChain(events);
   // No budget, streaming verifier, resume marker, or corrupt-chain recovery.
   ```

   Discipline: Hard rule #10 makes bounded budgets, streaming verification,
   and recovery primitives part of protocol-grade work.

10. **Cherry-pick conflict patterns**

    Failure mode: parallel agents touching the same file produced predictable
    cherry-pick conflicts during integration.

    ```text
    agent-a edits packages/protocol/AGENTS.md
    agent-b edits packages/protocol/AGENTS.md
    git cherry-pick agent-b  # conflict on the shared section
    ```

    Discipline: Hard rule #12 and "When you delegate" require a file-overlap
    matrix before dispatch and dependency-ordered cherry-picks afterward.

11. **Subpath exports bypass the index.ts gate**

    Failure mode: package subpath exports let consumers import around
    `src/index.ts`, so the public-API gate looks stricter than it is.

    ```json
    {
      "exports": {
        ".": "./src/index.ts",
        "./receipt": "./src/receipt.ts"
      }
    }
    ```

    Discipline: keep `exports` to `"."` only, or document every subpath as a
    deliberately stable public surface with the same review bar as `index.ts`.

12. **Demo importing from src/* is a fake gate**

    Failure mode: a demo that imports from submodules can pass while the
    package entrypoint is missing or mis-exporting the same API.

    ```ts
    import { receiptFromJson } from "../src/receipt.ts";
    // Missing export from ../src/index.ts stays invisible.
    receiptFromJson(sampleReceiptJson);
    ```

    Discipline: the demo MUST import from `src/index.ts` so it exercises the
    same package surface reviewers expect downstream consumers to use.

13. **Per-element budgets vs container budgets**

    Failure mode: a count cap protects against too many records but not one
    oversized record that exhausts memory during validation or hashing.

    ```ts
    assertAuditBatchLength(records, MAX_AUDIT_CHAIN_BATCH_SIZE);
    // Missing per-record MAX_AUDIT_EVENT_BODY_BYTES check.
    verifyChainIncremental(state, records);
    ```

    Discipline: every budget surface needs both axes when both can grow:
    container count and per-element byte size.

14. **Budget validators must compose into the entry-point validator**

    Failure mode: exporting a standalone budget validator leaves callers one
    forgotten function call away from accepting unbounded input.

    ```ts
    validateReceipt(receipt);
    // Caller forgot validateReceiptBudget(receipt).
    receiptToJson(receipt);
    ```

    Discipline: entry-point validators compose the budget check internally;
    standalone budget exports exist only for pre-deserialization screening.

15. **Wire-shape rejection at write time AND at verify time**

    Failure mode: a serializer rejects a new union value, but a verifier that
    trusts the already-written bytes lets hostile or buggy writers bypass it.

    ```ts
    payload: { kind: "future-kind", body: {} }
    // Writer rejects it; verifier never checks payload.kind.
    verifyAuditEventRecord(record);
    ```

    Discipline: every wire-side string union is runtime-validated by both the
    writer/serializer path and the verifier/reader path.

16. **Triangulation as the sweep tool for late-cycle reviews**

    Failure mode: repeated single-frame reviews can converge on confidence
    while still missing classes of drift that only another lens catches.

    ```text
    R1-R5: no blocking findings
    R6 seven-lens triangulation: public API, budgets, and verifier drift found
    ```

    Discipline: when protocol work looks well-reviewed, run orthogonal
    triangulation as the late-cycle sweep instead of treating that as done.

17. **Naming-convention drift survives review unless it's lint-enforced**

    Failure mode: snake_case fields in TS interfaces (mirroring a JSON wire
    shape) survived multiple rounds of review because reviewers pattern-match
    "matches the wire" as fine and don't realize the runtime API and the
    wire shape can be different shapes joined by a codec.

    ```text
    R1-R6: snake_case TS interface accepted because "wire is snake_case"
    R7: useNamingConvention enabled, ApiBootstrap.broker_url failed lint,
        codec moved to apiBootstrapFromJson/apiBootstrapToJson
    ```

    Discipline: naming-convention drift is exactly the kind of soft drift
    that lint catches and humans don't. Wire-format snake_case is fine in
    JSON; the TS surface stays camelCase and the codec at the boundary
    handles translation. Same pattern receipt codecs already used.
