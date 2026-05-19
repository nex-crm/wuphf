import type Database from "better-sqlite3";

export interface ThreadProjectionSnapshotRow {
  readonly threadId: string;
  readonly title: string;
  readonly status: string;
  readonly headLsn: number;
  readonly createdBy: string;
  readonly createdAtMs: number;
  readonly updatedAtMs: number;
  readonly closedAtMs: number | null;
  readonly specRevisionId: string | null;
  readonly specBaseRevisionId: string | null;
  readonly specContent: string | null;
  readonly specContentHash: string | null;
  readonly specAuthoredBy: string | null;
  readonly specAuthoredAtMs: number | null;
  readonly externalRefs: string;
}

export function snapshotThreadProjection(
  db: Database.Database,
): readonly ThreadProjectionSnapshotRow[] {
  return db
    .prepare<[], ThreadProjectionSnapshotRow>(
      `SELECT thread_id AS threadId,
              title,
              status,
              head_lsn AS headLsn,
              created_by AS createdBy,
              created_at_ms AS createdAtMs,
              updated_at_ms AS updatedAtMs,
              closed_at_ms AS closedAtMs,
              spec_revision_id AS specRevisionId,
              spec_base_revision_id AS specBaseRevisionId,
              spec_content AS specContent,
              spec_content_hash AS specContentHash,
              spec_authored_by AS specAuthoredBy,
              spec_authored_at_ms AS specAuthoredAtMs,
              external_refs AS externalRefs
       FROM threads
       ORDER BY thread_id ASC`,
    )
    .all();
}
