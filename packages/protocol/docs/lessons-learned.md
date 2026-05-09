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
