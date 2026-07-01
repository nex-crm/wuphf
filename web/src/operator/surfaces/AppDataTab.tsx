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
import {
  type CapabilityRow,
  deriveCapabilityRows,
  hasAnyCapability,
} from "../apps/appCapabilities";
import { useAppCapabilities } from "../apps/useOperatorApps";
import { EmptyState } from "../components/EmptyState";
import { Eyebrow } from "../components/primitives";

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

  const { reads, writes } = deriveCapabilityRows(caps);
  const bridgeApis = caps.bridge_apis ?? [];
  const readsTasks = bridgeApis.includes("getTasks");
  const readsEmails = bridgeApis.includes("getEmails");

  return (
    <div className="opr-tool-scoped opr-app-data">
      <div className="opr-data-intro">
        <Eyebrow>Data this app touches</Eyebrow>
        <p className="opr-scoped-note">
          Read from this app's actual code, not a guess. Apps do not keep a
          private database — they read your workspace data and write only with
          your approval.
        </p>
      </div>

      {reads.length > 0 ? (
        <CapabilitySection title="Reads" rows={reads} />
      ) : null}
      {writes.length > 0 ? (
        <CapabilitySection title="Writes" rows={writes} />
      ) : null}

      {caps.data_types && caps.data_types.length > 0 ? (
        <div className="opr-data-block">
          <div className="opr-data-block-head">Data model</div>
          <div className="opr-chip-row">
            {caps.data_types.map((t) => (
              <span className="opr-type-chip" key={t}>
                {t}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {readsTasks ? <TasksPreview /> : null}
      {readsEmails ? <EmailsPreview appId={appId} /> : null}
    </div>
  );
}

function CapabilitySection({
  title,
  rows,
}: {
  title: string;
  rows: CapabilityRow[];
}) {
  return (
    <div className="opr-data-block">
      <div className="opr-data-block-head">{title}</div>
      <ul className="opr-data-list">
        {rows.map((row) => (
          <li
            className="opr-data-row"
            key={`${row.label}|${row.detail}|${row.gated ? "w" : "r"}`}
          >
            <span className="opr-data-row-label">{row.label}</span>
            <span className="opr-data-row-detail">{row.detail}</span>
            {row.gated ? (
              <span className="opr-pill opr-pill-muted opr-data-row-gate">
                approval
              </span>
            ) : null}
          </li>
        ))}
      </ul>
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
}

// Best-effort normalizer over the GMAIL_FETCH_EMAILS payload (its envelope
// varies — a bare array, or { messages } / { data } — and per-email field names
// differ), so the preview shows real rows regardless of the exact shape.
function normalizeEmails(raw: unknown): EmailRow[] {
  const asArray = (v: unknown): unknown[] => {
    if (Array.isArray(v)) return v;
    const o = (v ?? {}) as { messages?: unknown; data?: unknown };
    if (Array.isArray(o.messages)) return o.messages;
    if (Array.isArray(o.data)) return o.data;
    return [];
  };
  const str = (v: unknown): string =>
    typeof v === "string" ? v : v == null ? "" : String(v);
  return asArray(raw)
    .slice(0, 12)
    .map((e, i) => {
      const o = (e ?? {}) as Record<string, unknown>;
      return {
        id: str(o.id ?? o.message_id ?? o.messageId ?? i),
        sender: str(o.sender ?? o.from ?? o.from_email ?? o.fromEmail).trim(),
        subject: str(o.subject).trim(),
        received: str(
          o.date ?? o.received_at ?? o.messageTimestamp ?? o.internalDate,
        ).trim(),
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
      <div className="opr-data-block-head">
        Live preview · Emails
        <span className="opr-data-block-sub">what the app sees right now</span>
      </div>
      {query.isLoading ? (
        <p className="opr-scoped-note">Reading your inbox…</p>
      ) : failed ? (
        <p className="opr-scoped-note">
          Could not read Gmail right now — connect it in Settings, or the
          workspace is offline.
        </p>
      ) : rows.length === 0 ? (
        <p className="opr-scoped-note">No recent emails in the window.</p>
      ) : (
        <table className="opr-data-table">
          <thead>
            <tr>
              <th>Sender</th>
              <th>Subject</th>
              <th>Received</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((e) => (
              <tr key={e.id}>
                <td>{e.sender || "—"}</td>
                <td>{e.subject || "—"}</td>
                <td>{e.received || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
