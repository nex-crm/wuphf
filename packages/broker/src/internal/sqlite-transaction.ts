import type { DatabaseSync } from "node:sqlite";

type TransactionVariant = "deferred" | "immediate" | "exclusive";

export interface TransactionFn<TArgs extends readonly unknown[], TResult> {
  (...args: TArgs): TResult;
  readonly deferred: (...args: TArgs) => TResult;
  readonly immediate: (...args: TArgs) => TResult;
  readonly exclusive: (...args: TArgs) => TResult;
}

export function createTransaction<TArgs extends readonly unknown[], TResult>(
  db: DatabaseSync,
  fn: (...args: TArgs) => TResult,
): TransactionFn<TArgs, TResult> {
  const run =
    (variant: TransactionVariant) =>
    (...args: TArgs): TResult => {
      const beginSql = variant === "deferred" ? "BEGIN" : `BEGIN ${variant.toUpperCase()}`;
      db.exec(beginSql);
      let committed = false;
      try {
        const result = fn(...args);
        db.exec("COMMIT");
        committed = true;
        return result;
      } finally {
        if (!committed) {
          try {
            db.exec("ROLLBACK");
          } catch {
            // db may already be in autocommit if BEGIN itself threw,
            // or the connection may be unusable. Swallow so the
            // original error reaches the caller.
          }
        }
      }
    };
  const deferred = run("deferred");
  const callable = ((...args: TArgs) => deferred(...args)) as TransactionFn<TArgs, TResult>;
  Object.defineProperty(callable, "deferred", { value: deferred });
  Object.defineProperty(callable, "immediate", { value: run("immediate") });
  Object.defineProperty(callable, "exclusive", { value: run("exclusive") });
  return callable;
}
