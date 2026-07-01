// AppDataTab — the Data tab for a built app, backed by REAL ground truth.
//
// Apps do not own a private data store; they are read-only renderers over
// workspace data plus gated writes. So "Data" is not a fake typed table — it is
// the deterministic, source-derived map of what THIS app actually reads and
// writes (GET /apps/{id}?source=1 -> capabilities, computed by introspectAppSource),
// with a live preview of the workspace collection it reads so the operator sees
// the real shape, not a mock.

import { useQuery } from "@tanstack/react-query";

import { get, post } from "../../api/client";
import type { Task } from "../../api/tasks";
import { hasAnyCapability } from "../apps/appCapabilities";
import { useAppCapabilities } from "../apps/useOperatorApps";
import { EmptyState } from "../components/EmptyState";

interface AppDataTabProps {
  appId: string;
}

export function AppDataTab({ appId }: AppDataTabProps) {
  const capsQuery = useAppCapabilities(appId);
  const caps = capsQuery.data;

  if (capsQuery.isLoading) {
    return (
      <div className="opr-app-building" role="status">
        <span className="opr-work-dots" aria-hidden={true}>
          <span />
          <span />
          <span />
        </span>
        <div className="opr-empty-title">Reading this app…</div>
      </div>
    );
  }

  if (!(caps && hasAnyCapability(caps))) {
    return (
      <EmptyState
        glyph="▦"
        title="No data access yet"
        hint="This app does not read workspace data or call an integration yet. When it does, exactly what it touches shows here — derived from its real code."
      />
    );
  }

  const bridgeApis = caps.bridge_apis ?? [];
  const readsEmails = bridgeApis.includes("getEmails");
  const readsTasks = bridgeApis.includes("getTasks");

  // Just the data — a table of the real rows this app reads. No capability map,
  // no chips, no prose: the Data tab IS the table.
  return (
    <div className="opr-tool-scoped opr-app-data">
      {readsEmails ? (
        <EmailsPreview appId={appId} />
      ) : readsTasks ? (
        <TasksPreview />
      ) : (
        <EmptyState
          glyph="▦"
          title="No table yet"
          hint="This app does not read a workspace collection (email or tasks) yet. When it does, the rows it reads show here as a table."
        />
      )}
    </div>
  );
}

// Live preview of the workspace tasks this app reads, so "Data" shows real rows,
// not a placeholder. Read-only and best-effort: a fetch failure degrades to a
// quiet note rather than blocking the capability map above it.
function TasksPreview() {
  const query = useQuery({
    queryKey: ["operator-app-data-tasks"],
    queryFn: () =>
      get<{ tasks?: Task[] }>(
        "/tasks?all_channels=true&include_done=true&viewer_slug=human",
      ),
  });
  const tasks = (query.data?.tasks ?? []).slice(0, 6);

  return (
    <div className="opr-data-block">
      <div className="opr-data-block-head">
        Live preview · Tasks
        <span className="opr-data-block-sub">what the app sees right now</span>
      </div>
      {query.isError ? (
        <p className="opr-scoped-note">
          The workspace is offline, so the live preview is unavailable.
        </p>
      ) : tasks.length === 0 ? (
        <p className="opr-scoped-note">No tasks in the workspace yet.</p>
      ) : (
        <table className="opr-data-table">
          <thead>
            <tr>
              <th>Task</th>
              <th>Status</th>
              <th>Owner</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((t) => (
              <tr key={t.id}>
                <td>{t.title}</td>
                <td>
                  <span className="opr-pill opr-pill-muted">{t.status}</span>
                </td>
                <td>{t.owner || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

interface EmailBridgeResult {
  result?: unknown;
  error?: string;
}

interface EmailRow {
  id: string;
  sender: string;
  subject: string;
  received: string;
  preview: string;
}

// The columns of the app's email data model + the type shown in each header.
const EMAIL_COLUMNS: { key: keyof EmailRow; label: string; type: string }[] = [
  { key: "sender", label: "Sender", type: "string" },
  { key: "subject", label: "Subject", type: "string" },
  { key: "received", label: "Received", type: "date" },
  { key: "preview", label: "Preview", type: "string" },
];

// Best-effort normalizer over the GMAIL_FETCH_EMAILS payload. The FE receives
// `result`, which is `{ data: { messages: [...] }, ... }`; per-email field names
// vary, so pull the common ones defensively.
function normalizeEmails(raw: unknown): EmailRow[] {
  const asArray = (v: unknown): unknown[] => {
    if (Array.isArray(v)) return v;
    const o = (v ?? {}) as { messages?: unknown; data?: unknown };
    if (Array.isArray(o.messages)) return o.messages;
    const data = (o.data ?? {}) as { messages?: unknown };
    if (Array.isArray(data.messages)) return data.messages;
    if (Array.isArray(o.data)) return o.data;
    return [];
  };
  const str = (v: unknown): string =>
    typeof v === "string" ? v : v == null ? "" : String(v);
  const clip = (s: string, n: number): string =>
    s.length > n ? `${s.slice(0, n)}…` : s;
  return asArray(raw)
    .slice(0, 12)
    .map((e, i) => {
      const o = (e ?? {}) as Record<string, unknown>;
      return {
        id: str(o.id ?? o.message_id ?? o.messageId ?? i),
        sender: clip(
          str(o.sender ?? o.from ?? o.from_email ?? o.fromEmail).trim(),
          40,
        ),
        subject: clip(str(o.subject).trim(), 60),
        received: str(
          o.date ?? o.received_at ?? o.messageTimestamp ?? o.internalDate,
        ).trim(),
        preview: clip(
          str(o.snippet ?? o.messageText ?? o.preview)
            .replace(/\s+/g, " ")
            .trim(),
          70,
        ),
      };
    });
}

// Live preview of the real emails this app reads through the Gmail bridge, so
// "Data" shows the actual rows + columns the app renders, not just a capability
// map. Read-only and best-effort: a failure degrades to a quiet note.
function EmailsPreview({ appId }: { appId: string }) {
  const query = useQuery({
    queryKey: ["operator-app-data-emails", appId],
    queryFn: () =>
      post<EmailBridgeResult>("/apps/integrations/call", {
        platform: "gmail",
        action: "GMAIL_FETCH_EMAILS",
        params: { query: "newer_than:7d", max_results: 12 },
        app_id: appId,
      }),
  });
  const rows = normalizeEmails(query.data?.result);
  const failed = query.isError || Boolean(query.data?.error);

  return (
    <div className="opr-data-block">
      {query.isLoading ? (
        <p className="opr-scoped-note">Reading the app's data…</p>
      ) : failed ? (
        <p className="opr-scoped-note">
          Could not read the data right now — connect Gmail in Settings, or the
          workspace is offline.
        </p>
      ) : rows.length === 0 ? (
        <p className="opr-scoped-note">No records yet.</p>
      ) : (
        <table className="opr-data-table">
          <thead>
            <tr>
              {EMAIL_COLUMNS.map((c) => (
                <th key={c.key}>
                  <span className="opr-data-col-name">{c.label}</span>
                  <span className="opr-data-col-type">{c.type}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((e) => (
              <tr key={e.id}>
                {EMAIL_COLUMNS.map((c) => (
                  <td key={c.key}>{e[c.key] || "—"}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
