/**
 * bridgeDataProvider — refine's DataProvider implemented over wuphf-bridge.ts.
 *
 * This is what lets every refine data hook (`useList`, `useTable`, `useOne`,
 * `useShow`, `useForm`, …) read and write WUPHF workspace data WITHOUT any HTTP.
 * refine's data layer is transport-agnostic: a DataProvider is just an object of
 * async methods. Here each method routes through the postMessage bridge — the
 * ONLY channel out of the sealed sandbox (opaque origin, CSP connect-src 'none',
 * so fetch/XHR/WebSocket are all blocked).
 *
 * Resource → bridge mapping (this is the whole contract for `useTable`/`useList`):
 *   "tasks"        → getTasks()            (all office tasks, every channel)
 *   "members"      → getOfficeMembers()    (the office roster)
 *   "emails"       → getEmails()           (read-only Gmail, if connected)
 *   "integrations" → listIntegrations()    (connected tools + their READ actions)
 *   <other>        → callIntegration(platform, action, params) via getList meta
 *
 * The bridge is READ-MOSTLY: every reader returns a whole array, so this provider
 * does filtering / sorting / pagination CLIENT-SIDE in memory (internal-tool
 * datasets are small). The single write is create("tasks") → createTask(), which
 * the host gates behind a human confirmation. update/delete are NOT supported and
 * throw loudly so a coding agent gets a clear error instead of a silent no-op.
 *
 * PROTECTED FILE — like wuphf-bridge.ts, import and use this; do not reimplement
 * it. It is already correct (e.g. getTasks() reads ALL channels). See AI_RULES.md.
 */
import type {
  BaseRecord,
  CrudFilter,
  CrudSort,
  DataProvider,
  GetListParams,
} from "@refinedev/core";

import {
  callIntegration,
  createTask,
  getEmails,
  getOfficeMembers,
  getTasks,
} from "./wuphf-bridge";

/** A bridge reader returns the full list for a resource as plain records. */
type Reader = (params: GetListParams) => Promise<BaseRecord[]>;

/**
 * Built-in resource readers. Each maps a refine resource name to the bridge call
 * that returns its whole list. Add your own by passing
 * `meta: { platform, action, params }` to a resource and reading "integration"
 * (see `integrationReader`), or extend this map for a bespoke named resource.
 */
const readers: Record<string, Reader> = {
  tasks: async () => (await getTasks()).tasks as unknown as BaseRecord[],
  members: async () =>
    (await getOfficeMembers()).members as unknown as BaseRecord[],
  emails: async () => {
    const { connected, emails, error } = await getEmails({ limit: 50 });
    if (!connected) {
      // Surface a connect-state, not a crash. The app should render a
      // "connect Gmail" panel; an empty list keeps useTable happy meanwhile.
      return [];
    }
    if (error) {
      // Connected but the read failed: throw so refine's tableQuery.error
      // populates and the app renders a real error state (not a silent empty
      // list). getEmails never throws on its own — it returns this {error}.
      throw new Error(error);
    }
    return emails as unknown as BaseRecord[];
  },
};

/**
 * integrationReader handles ANY connected integration as a refine resource. Use
 * it by declaring a resource whose name is the integration action and passing
 * `meta: { platform, action, params }` — e.g. useList({ resource: "slack-msgs",
 * meta: { platform: "slack", action: "SLACK_FETCH_MESSAGES", params: {…} } }).
 * Only READ actions return data; a mutating action comes back needing approval
 * and yields an empty list here (the host raises the approval card separately).
 */
async function integrationReader(params: GetListParams): Promise<BaseRecord[]> {
  const meta = params.meta as
    | { platform?: string; action?: string; params?: Record<string, unknown> }
    | undefined;
  if (!meta?.platform || !meta?.action) {
    throw new Error(
      `Unknown resource "${params.resource}". Use a built-in resource ` +
        `(tasks/members/emails) or pass meta:{ platform, action } for an integration.`,
    );
  }
  const res = await callIntegration(meta.platform, meta.action, meta.params);
  if (!res.connected || res.status !== "ok") return [];
  const result = res.result as
    | { data?: { items?: unknown[]; messages?: unknown[] } }
    | unknown[]
    | undefined;
  if (Array.isArray(result)) return result as BaseRecord[];
  const items = result?.data?.items ?? result?.data?.messages;
  return Array.isArray(items) ? (items as BaseRecord[]) : [];
}

function readFor(params: GetListParams): Promise<BaseRecord[]> {
  const reader = readers[params.resource];
  return reader ? reader(params) : integrationReader(params);
}

function matches(value: unknown, filter: CrudFilter): boolean {
  if (!("field" in filter)) return true; // skip and/or conditional groups in v1
  const v = value;
  switch (filter.operator) {
    case "eq":
      return v === filter.value;
    case "ne":
      return v !== filter.value;
    case "contains":
      return String(v ?? "")
        .toLowerCase()
        .includes(String(filter.value).toLowerCase());
    case "ncontains":
      return !String(v ?? "")
        .toLowerCase()
        .includes(String(filter.value).toLowerCase());
    case "gt":
      return Number(v) > Number(filter.value);
    case "gte":
      return Number(v) >= Number(filter.value);
    case "lt":
      return Number(v) < Number(filter.value);
    case "lte":
      return Number(v) <= Number(filter.value);
    default:
      return true; // unknown operators are no-ops, never a silent drop-all
  }
}

function applyFilters(rows: BaseRecord[], filters?: CrudFilter[]): BaseRecord[] {
  if (!filters?.length) return rows;
  return rows.filter((row) =>
    filters.every((f) =>
      "field" in f ? matches(row[f.field], f) : true,
    ),
  );
}

function applySorters(rows: BaseRecord[], sorters?: CrudSort[]): BaseRecord[] {
  if (!sorters?.length) return rows;
  // Stable multi-key sort: later sorters break ties of earlier ones.
  return [...rows].sort((a, b) => {
    for (const { field, order } of sorters) {
      const dir = order === "desc" ? -1 : 1;
      const av = a[field];
      const bv = b[field];
      if (av === bv) continue;
      if (av === undefined || av === null) return 1;
      if (bv === undefined || bv === null) return -1;
      return av > bv ? dir : -dir;
    }
    return 0;
  });
}

/** A workspace write the bridge actually supports (only createTask today). */
interface CreateTaskVariables {
  title: string;
  details?: string;
}

/**
 * The bridge data provider. Pass it straight to <Refine dataProvider={…}>. Pure
 * async over postMessage — no fetch, no HTTP, no storage. Read-mostly: getList /
 * getOne / getMany work for every resource; create("tasks") is the one write;
 * update / deleteOne throw because apps cannot mutate workspace data.
 */
export const bridgeDataProvider: DataProvider = {
  getList: async <TData extends BaseRecord = BaseRecord>(
    params: GetListParams,
  ) => {
    const all = applySorters(
      applyFilters(await readFor(params), params.filters),
      params.sorters,
    );
    const total = all.length;
    const { currentPage = 1, pageSize = 10, mode = "server" } =
      params.pagination ?? {};
    const data =
      mode === "off"
        ? all
        : all.slice((currentPage - 1) * pageSize, currentPage * pageSize);
    return { data: data as unknown as TData[], total };
  },

  getOne: async <TData extends BaseRecord = BaseRecord>(params: {
    resource: string;
    id: string | number;
    meta?: Record<string, unknown>;
  }) => {
    const all = await readFor(params as unknown as GetListParams);
    const found = all.find((r) => String(r.id) === String(params.id));
    if (!found) {
      throw new Error(`Not found: ${params.resource}/${params.id}`);
    }
    return { data: found as unknown as TData };
  },

  getMany: async <TData extends BaseRecord = BaseRecord>(params: {
    resource: string;
    ids: (string | number)[];
    meta?: Record<string, unknown>;
  }) => {
    const all = await readFor(params as unknown as GetListParams);
    const wanted = new Set(params.ids.map(String));
    const data = all.filter((r) => wanted.has(String(r.id)));
    return { data: data as unknown as TData[] };
  },

  // The single human-gated write. Map create("tasks") onto createTask(); reject
  // every other resource loudly so the agent gets a clear, actionable error.
  create: async <TData extends BaseRecord = BaseRecord, TVariables = object>(params: {
    resource: string;
    variables: TVariables;
    meta?: Record<string, unknown>;
  }) => {
    if (params.resource !== "tasks") {
      throw new Error(
        `Apps can only create tasks (got create '${params.resource}'). ` +
          `All other writes are unsupported in a WUPHF App.`,
      );
    }
    const v = params.variables as CreateTaskVariables;
    if (!v?.title) throw new Error("create('tasks') requires a `title`.");
    const created = await createTask({ title: v.title, details: v.details });
    return { data: created as unknown as TData };
  },

  update: async () => {
    throw new Error(
      "Apps cannot update workspace data — the bridge is read-mostly.",
    );
  },

  deleteOne: async () => {
    throw new Error(
      "Apps cannot delete workspace data — the bridge is read-mostly.",
    );
  },

  // No HTTP base URL: the bridge is the transport. refine only uses this string
  // for HTTP-style providers; ours returns empty.
  getApiUrl: () => "",
};
