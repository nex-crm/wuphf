import { readFileSync } from "node:fs";

import type Database from "better-sqlite3";

export const CURRENT_SCHEMA_VERSION = 1;

interface Migration {
  readonly version: number;
  readonly sql: string;
}

const MIGRATIONS: readonly Migration[] = [
  {
    version: 1,
    sql: readFileSync(new URL("./001_initial.sql", import.meta.url), "utf8"),
  },
];

export function runMigrations(db: Database.Database): void {
  db.pragma("foreign_keys = ON");

  // Read `user_version` AFTER acquiring an EXCLUSIVE write lock and
  // apply pending migrations inside the same transaction. Otherwise
  // two broker processes opening a fresh DB concurrently can both
  // observe `user_version = 0`, race the `CREATE TABLE` statements,
  // and one or both fail mid-startup with "table already exists" or
  // lock errors. EXCLUSIVE blocks readers and writers; cheap because
  // migrations only run on schema upgrades.
  const applyPending = db.transaction(() => {
    const currentVersion = readUserVersion(db);
    if (currentVersion > CURRENT_SCHEMA_VERSION) {
      throw new Error(
        `Database schema version ${currentVersion} is newer than supported version ${CURRENT_SCHEMA_VERSION}`,
      );
    }
    for (const migration of MIGRATIONS) {
      if (migration.version <= currentVersion) continue;
      db.exec(migration.sql);
      db.pragma(`user_version = ${migration.version}`);
    }
  });
  applyPending.exclusive();
}

function readUserVersion(db: Database.Database): number {
  const value = db.pragma("user_version", { simple: true });
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) {
    throw new Error(`Invalid SQLite user_version: ${String(value)}`);
  }
  return value;
}
