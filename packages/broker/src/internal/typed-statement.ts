import type { StatementSync } from "node:sqlite";

type RunArgs = Parameters<StatementSync["run"]>;
type GetArgs = Parameters<StatementSync["get"]>;
type AllArgs = Parameters<StatementSync["all"]>;
type IterateArgs = Parameters<StatementSync["iterate"]>;

export interface TypedStatement<TParams extends readonly unknown[], TRow> {
  readonly inner: StatementSync;
  run(...args: TParams): { changes: number; lastInsertRowid: number | bigint };
  get(...args: TParams): TRow | undefined;
  all(...args: TParams): readonly TRow[];
  iterate(...args: TParams): IterableIterator<TRow>;
}

export function typed<TParams extends readonly unknown[], TRow>(
  stmt: StatementSync,
): TypedStatement<TParams, TRow> {
  return {
    inner: stmt,
    run: (...args: TParams) =>
      stmt.run(...(args as unknown as RunArgs)) as {
        changes: number;
        lastInsertRowid: number | bigint;
      },
    get: (...args: TParams) => stmt.get(...(args as unknown as GetArgs)) as TRow | undefined,
    all: (...args: TParams) =>
      stmt.all(...(args as unknown as AllArgs)) as unknown as readonly TRow[],
    iterate: (...args: TParams) =>
      stmt.iterate(...(args as unknown as IterateArgs)) as IterableIterator<TRow>,
  };
}
