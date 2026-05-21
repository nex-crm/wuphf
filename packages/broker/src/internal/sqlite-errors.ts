interface SqliteErrorLike extends Error {
  readonly errcode: number;
}

const SQLITE_BASE_MASK = 0xff;
const SQLITE_PERM = 3;
const SQLITE_BUSY = 5;
const SQLITE_LOCKED = 6;
const SQLITE_READONLY = 8;
const SQLITE_IOERR = 10;
const SQLITE_CORRUPT = 11;
const SQLITE_FULL = 13;
const SQLITE_CANTOPEN = 14;
const SQLITE_CONSTRAINT = 19;
const SQLITE_NOTADB = 26;

export function isSqliteError(err: unknown): err is SqliteErrorLike {
  if (!(err instanceof Error)) return false;
  const candidate = err as { readonly errcode?: unknown };
  return typeof candidate.errcode === "number" && Number.isInteger(candidate.errcode);
}

export function isSqliteConstraintError(err: unknown): boolean {
  return sqliteBaseCode(err) === SQLITE_CONSTRAINT;
}

export function isSqliteFullError(err: unknown): boolean {
  return sqliteBaseCode(err) === SQLITE_FULL;
}

export function isSqliteBusyError(err: unknown): boolean {
  const code = sqliteBaseCode(err);
  return code === SQLITE_BUSY || code === SQLITE_LOCKED;
}

export function isSqliteUnavailableError(err: unknown): boolean {
  const code = sqliteBaseCode(err);
  return (
    code === SQLITE_READONLY ||
    code === SQLITE_CANTOPEN ||
    code === SQLITE_CORRUPT ||
    code === SQLITE_NOTADB ||
    code === SQLITE_PERM ||
    code === SQLITE_IOERR
  );
}

export function sqliteErrorLabel(err: unknown): string {
  const code = sqliteBaseCode(err);
  if (code === null) return "unknown";
  if (code === SQLITE_PERM) return "SQLITE_PERM";
  if (code === SQLITE_BUSY) return "SQLITE_BUSY";
  if (code === SQLITE_LOCKED) return "SQLITE_LOCKED";
  if (code === SQLITE_READONLY) return "SQLITE_READONLY";
  if (code === SQLITE_IOERR) return "SQLITE_IOERR";
  if (code === SQLITE_CORRUPT) return "SQLITE_CORRUPT";
  if (code === SQLITE_FULL) return "SQLITE_FULL";
  if (code === SQLITE_CANTOPEN) return "SQLITE_CANTOPEN";
  if (code === SQLITE_CONSTRAINT) return "SQLITE_CONSTRAINT";
  if (code === SQLITE_NOTADB) return "SQLITE_NOTADB";
  return `SQLITE_${code}`;
}

function sqliteBaseCode(err: unknown): number | null {
  if (!isSqliteError(err)) return null;
  return err.errcode & SQLITE_BASE_MASK;
}
