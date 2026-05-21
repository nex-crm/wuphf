import { DatabaseSync } from "node:sqlite";

import { describe, expect, it } from "vitest";

import { createTransaction } from "../../src/internal/sqlite-transaction.ts";

interface CountRow {
  readonly count: number;
}

interface NameRow {
  readonly name: string;
}

interface RecordingDb {
  isTransaction: boolean;
  readonly execSql: string[];
  exec(sql: string): void;
}

function createRecordingDb(isTransaction = false): RecordingDb {
  return {
    isTransaction,
    execSql: [],
    exec(sql: string): void {
      this.execSql.push(sql);
      if (sql.startsWith("BEGIN") || sql.startsWith("SAVEPOINT ")) {
        this.isTransaction = true;
      }
      if (sql === "COMMIT" || sql === "ROLLBACK" || sql.startsWith("RELEASE SAVEPOINT ")) {
        this.isTransaction = false;
      }
    },
  };
}

function asDatabase(db: RecordingDb): DatabaseSync {
  return db as unknown as DatabaseSync;
}

function openItemsDb(): DatabaseSync {
  const db = new DatabaseSync(":memory:");
  db.exec("CREATE TABLE items (name TEXT PRIMARY KEY)");
  return db;
}

function countItems(db: DatabaseSync): number {
  return (
    (db.prepare("SELECT COUNT(*) AS count FROM items").get() as CountRow | undefined)?.count ?? 0
  );
}

function itemNames(db: DatabaseSync): readonly string[] {
  return (db.prepare("SELECT name FROM items ORDER BY name ASC").all() as unknown as NameRow[]).map(
    (row) => row.name,
  );
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

describe("createTransaction", () => {
  it("default callable commits on success and passes args through", () => {
    const recording = createRecordingDb();
    const tx = createTransaction(asDatabase(recording), (left: number, right: number) => {
      recording.execSql.push("body");
      return left + right;
    });

    expect(tx(2, 3)).toBe(5);
    expect(recording.execSql).toEqual(["BEGIN", "body", "COMMIT"]);
  });

  it("deferred, immediate, and exclusive variants emit the expected BEGIN", () => {
    const cases = [
      { name: "deferred", begin: "BEGIN" },
      { name: "immediate", begin: "BEGIN IMMEDIATE" },
      { name: "exclusive", begin: "BEGIN EXCLUSIVE" },
    ] as const;

    for (const testCase of cases) {
      const recording = createRecordingDb();
      const tx = createTransaction(asDatabase(recording), () => {
        recording.execSql.push("body");
      });

      tx[testCase.name]();

      expect(recording.execSql).toEqual([testCase.begin, "body", "COMMIT"]);
    }
  });

  it("rolls back on synchronous throw", () => {
    const db = openItemsDb();
    try {
      const insert = db.prepare("INSERT INTO items (name) VALUES (?)");
      const tx = createTransaction(db, () => {
        insert.run("rolled-back");
        throw new Error("boom");
      });

      expect(() => tx.immediate()).toThrow("boom");
      expect(countItems(db)).toBe(0);
    } finally {
      db.close();
    }
  });

  it("documents the sync-only contract for promise-returning callbacks", async () => {
    const db = openItemsDb();
    try {
      const insert = db.prepare("INSERT INTO items (name) VALUES (?)");
      const tx = createTransaction(db, async () => {
        insert.run("committed-before-rejection");
        await Promise.resolve();
        throw new Error("late failure");
      });

      await expect(tx.immediate()).rejects.toThrow("late failure");
      expect(itemNames(db)).toEqual(["committed-before-rejection"]);
    } finally {
      db.close();
    }
  });

  it("surfaces the original closed-database BEGIN failure", () => {
    const db = new DatabaseSync(":memory:");
    db.close();
    let directMessage = "";
    try {
      db.exec("BEGIN");
    } catch (err) {
      directMessage = errorMessage(err);
    }

    const tx = createTransaction(db, () => undefined);
    let transactionMessage = "";
    try {
      tx.immediate();
    } catch (err) {
      transactionMessage = errorMessage(err);
    }

    expect(transactionMessage).toBe(directMessage);
  });

  it("commits nested savepoints when outer and inner both succeed", () => {
    const db = openItemsDb();
    try {
      const insert = db.prepare("INSERT INTO items (name) VALUES (?)");
      const inner = createTransaction(db, (name: string) => {
        insert.run(name);
      });
      const outer = createTransaction(db, () => {
        insert.run("outer");
        inner.deferred("inner");
      });

      outer.immediate();

      expect(itemNames(db)).toEqual(["inner", "outer"]);
    } finally {
      db.close();
    }
  });

  it("rolls back an inner savepoint while allowing the outer transaction to continue", () => {
    const db = openItemsDb();
    try {
      const insert = db.prepare("INSERT INTO items (name) VALUES (?)");
      const inner = createTransaction(db, () => {
        insert.run("inner");
        throw new Error("inner failed");
      });
      const outer = createTransaction(db, () => {
        try {
          inner.deferred();
        } catch (err) {
          expect(errorMessage(err)).toBe("inner failed");
        }
        insert.run("outer");
      });

      outer.immediate();

      expect(itemNames(db)).toEqual(["outer"]);
    } finally {
      db.close();
    }
  });

  it("outer rollback still rolls back changes from a released inner savepoint", () => {
    const db = openItemsDb();
    try {
      const insert = db.prepare("INSERT INTO items (name) VALUES (?)");
      const inner = createTransaction(db, () => {
        insert.run("inner");
      });
      const outer = createTransaction(db, () => {
        inner.deferred();
        throw new Error("outer failed");
      });

      expect(() => outer.immediate()).toThrow("outer failed");
      expect(countItems(db)).toBe(0);
    } finally {
      db.close();
    }
  });

  it("uses SAVEPOINT for nested variants", () => {
    const recording = createRecordingDb(true);
    const tx = createTransaction(asDatabase(recording), () => {
      recording.execSql.push("body");
    });

    tx.exclusive();

    expect(recording.execSql).toEqual([
      "SAVEPOINT wuphf_tx_0",
      "body",
      "RELEASE SAVEPOINT wuphf_tx_0",
    ]);
  });
});
