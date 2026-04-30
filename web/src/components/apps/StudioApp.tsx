import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle,
  Code,
  Component,
  Eye,
  Page,
  PlusCircle,
  Refresh,
  TableRows,
  TaskList,
  TextSquare,
  WarningTriangle,
} from "iconoir-react";

import {
  createSurface,
  listSurfaces,
  readSurface,
  subscribeSurfaceEvents,
  type NormalizedWidget,
  type SurfaceHistoryEntry,
  type SurfaceWidgetRecord,
} from "../../api/surfaces";
import { formatRelativeTime } from "../../lib/format";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";

export function StudioApp() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const queryClient = useQueryClient();
  const [selectedSurfaceId, setSelectedSurfaceId] = useState<string | null>(
    null,
  );
  const [selectedWidgetId, setSelectedWidgetId] = useState<string | null>(null);

  const surfacesQuery = useQuery({
    queryKey: ["surfaces"],
    queryFn: listSurfaces,
    refetchInterval: 15_000,
  });

  const surfaces = surfacesQuery.data?.surfaces ?? [];

  useEffect(() => {
    if (selectedSurfaceId && surfaces.some((s) => s.id === selectedSurfaceId)) {
      return;
    }
    const channelMatch = surfaces.find((s) => s.channel === currentChannel);
    setSelectedSurfaceId(channelMatch?.id ?? surfaces[0]?.id ?? null);
  }, [currentChannel, selectedSurfaceId, surfaces]);

  const detailQuery = useQuery({
    queryKey: ["surface-detail", selectedSurfaceId],
    queryFn: () => readSurface(selectedSurfaceId ?? ""),
    enabled: selectedSurfaceId !== null,
  });

  const detail = detailQuery.data ?? null;
  const widgets = detail?.widgets ?? [];

  useEffect(() => {
    if (selectedWidgetId && widgets.some((w) => w.widget.id === selectedWidgetId))
      return;
    setSelectedWidgetId(widgets[0]?.widget.id ?? null);
  }, [selectedWidgetId, widgets]);

  useEffect(() => {
    return subscribeSurfaceEvents((event) => {
      if (event.type === "surface:created" || event.type === "surface:deleted") {
        void queryClient.invalidateQueries({ queryKey: ["surfaces"] });
      }
      if (event.type === "surface:updated") {
        void queryClient.invalidateQueries({ queryKey: ["surfaces"] });
        if (event.surface_id === selectedSurfaceId) {
          void queryClient.invalidateQueries({ queryKey: ["surface-detail", event.surface_id] });
        }
      }
      if (
        (event.type === "surface:widget_created" ||
          event.type === "surface:widget_updated" ||
          event.type === "surface:render_checked") &&
        event.surface_id === selectedSurfaceId
      ) {
        void queryClient.invalidateQueries({ queryKey: ["surface-detail", event.surface_id] });
      }
    });
  }, [queryClient, selectedSurfaceId]);

  const selectedWidget = useMemo(
    () => widgets.find((w) => w.widget.id === selectedWidgetId) ?? null,
    [selectedWidgetId, widgets],
  );

  const createMutation = useMutation({
    mutationFn: () =>
      createSurface({
        title: nextSurfaceTitle(currentChannel || "general", surfaces),
        channel: currentChannel || "general",
      }),
    onSuccess: async (result) => {
      setSelectedSurfaceId(result.surface.id);
      await queryClient.invalidateQueries({ queryKey: ["surfaces"] });
      await queryClient.invalidateQueries({ queryKey: ["messages"] });
    },
    onError: (err: Error) => showNotice(err.message, "error"),
  });

  return (
    <div className="studio-surface" data-testid="studio-app">
      <header className="studio-toolbar">
        <div className="studio-toolbar-title">
          <Component />
          <div>
            <h1>Studio</h1>
            <span>Artifact room for #{currentChannel || "general"}</span>
          </div>
        </div>
        <div className="studio-toolbar-actions">
          <button
            type="button"
            className="studio-icon-button"
            aria-label="Refresh Studio"
            title="Refresh"
            onClick={() => {
              void queryClient.invalidateQueries({ queryKey: ["surfaces"] });
              void queryClient.invalidateQueries({
                queryKey: ["surface-detail"],
              });
            }}
          >
            <Refresh />
          </button>
          <button
            type="button"
            className="studio-command-button"
            onClick={() => createMutation.mutate()}
            disabled={createMutation.isPending}
          >
            <PlusCircle />
            <span>{createMutation.isPending ? "Creating" : "New surface"}</span>
          </button>
        </div>
      </header>

      <div className="studio-grid">
        <aside className="studio-list" aria-label="Surfaces">
          <div className="studio-panel-head">
            <div>
              <h2>Surfaces</h2>
              <span>
                {surfaces.length} {pluralize(surfaces.length, "room")}
              </span>
            </div>
          </div>
          {surfacesQuery.isLoading && <div className="studio-muted">Loading...</div>}
          {surfacesQuery.error && (
            <div className="studio-error">Could not load surfaces.</div>
          )}
          {!surfacesQuery.isLoading && surfaces.length === 0 && (
            <StudioEmptyState
              icon={<Component />}
              title="No surfaces yet."
              copy="The conference room is booked, allegedly."
            />
          )}
          {surfaces.map((surface) => (
            <button
              type="button"
              key={surface.id}
              className={`studio-list-item${
                surface.id === selectedSurfaceId ? " is-active" : ""
              }`}
              onClick={() => setSelectedSurfaceId(surface.id)}
            >
              <span className="studio-list-title">{surface.title}</span>
              <span className="studio-list-meta">
                #{surface.channel} / {surface.widget_count ?? 0}{" "}
                {pluralize(surface.widget_count ?? 0, "widget")}
              </span>
            </button>
          ))}
        </aside>

        <main className="studio-canvas" aria-label="Surface canvas">
          {detailQuery.isLoading && <div className="studio-muted">Loading...</div>}
          {detailQuery.error && (
            <div className="studio-error">Could not load this surface.</div>
          )}
          {!detailQuery.isLoading && !detail && (
            <StudioEmptyState
              icon={<Page />}
              title="Select a surface."
              copy="Pick a room to inspect what the agents left behind."
            />
          )}
          {detail && (
            <>
              <div className="studio-surface-header">
                <div>
                  <div className="studio-kicker">Studio surface</div>
                  <h2>{detail.surface.title}</h2>
                  <span>
                    #{detail.surface.channel} / updated{" "}
                    {formatRelativeTime(detail.surface.updated_at)}
                  </span>
                </div>
                <span className="badge badge-accent">
                  {widgets.length} {pluralize(widgets.length, "widget")}
                </span>
              </div>

              <div className="studio-widget-grid">
                {widgets.length === 0 && (
                  <StudioEmptyState
                    icon={<Component />}
                    title="No widgets yet."
                    copy="The room has a name. That is currently doing most of the work."
                  />
                )}
                {widgets.map((record) => (
                  <button
                    type="button"
                    key={record.widget.id}
                    className={`studio-widget-card${
                      record.widget.id === selectedWidgetId ? " is-selected" : ""
                    }`}
                    onClick={() => setSelectedWidgetId(record.widget.id)}
                  >
                    <WidgetCard record={record} />
                  </button>
                ))}
              </div>

              <ActivityFeed entries={detail.history ?? []} />
            </>
          )}
        </main>

        <aside className="studio-inspector" aria-label="Widget inspector">
          <Inspector record={selectedWidget} />
        </aside>
      </div>
    </div>
  );
}

function pluralize(count: number, one: string, many = `${one}s`) {
  return count === 1 ? one : many;
}

function nextSurfaceTitle(
  channel: string,
  surfaces: Array<{ title: string; channel: string }>,
) {
  const base = `${channel} command center`;
  const taken = new Set(
    surfaces
      .filter((surface) => surface.channel === channel)
      .map((surface) => surface.title),
  );
  if (!taken.has(base)) return base;
  for (let index = 2; ; index += 1) {
    const candidate = `${base} ${index}`;
    if (!taken.has(candidate)) return candidate;
  }
}

function WidgetCard({ record }: { record: SurfaceWidgetRecord }) {
  const normalized = record.render.normalized_widget;
  const hasError =
    record.render.errors && record.render.errors.length > 0 && !record.render.render_ok;
  return (
    <>
      <div className="studio-widget-head">
        <div className="studio-widget-heading">
          <WidgetKindIcon kind={record.widget.kind} />
          <div>
            <div className="studio-widget-title">{record.widget.title}</div>
            <div className="studio-widget-meta">{record.widget.kind}</div>
          </div>
        </div>
        <span
          className={`studio-render-pill${hasError ? " is-error" : " is-ok"}`}
          aria-label={hasError ? "Render has errors" : "Render OK"}
        >
          {hasError ? (
            <WarningTriangle className="studio-widget-status is-error" />
          ) : (
            <CheckCircle className="studio-widget-status" />
          )}
        </span>
      </div>
      {record.widget.description && (
        <p className="studio-widget-description">{record.widget.description}</p>
      )}
      <div className="studio-widget-body">
        {normalized ? (
          <NormalizedWidgetView widget={normalized} compact />
        ) : (
          <pre className="studio-preview">{record.render.preview_text}</pre>
        )}
      </div>
    </>
  );
}

function WidgetKindIcon({ kind }: { kind: string }) {
  if (kind === "checklist") return <TaskList className="studio-kind-icon" />;
  if (kind === "table") return <TableRows className="studio-kind-icon" />;
  if (kind === "markdown") return <TextSquare className="studio-kind-icon" />;
  return <Component className="studio-kind-icon" />;
}

function StudioEmptyState({
  icon,
  title,
  copy,
}: {
  icon: ReactNode;
  title: string;
  copy?: string;
}) {
  return (
    <div className="studio-empty">
      <div className="studio-empty-icon">{icon}</div>
      <strong>{title}</strong>
      {copy && <span>{copy}</span>}
    </div>
  );
}

function ActivityFeed({ entries }: { entries: SurfaceHistoryEntry[] }) {
  if (entries.length === 0) return null;
  const visibleEntries = entries.slice(0, 5);
  return (
    <section className="studio-activity" aria-label="Recent Studio activity">
      <div className="studio-activity-head">
        <h3>Recent activity</h3>
        <span>
          {entries.length} {pluralize(entries.length, "receipt")}
        </span>
      </div>
      <div className="studio-activity-list">
        {visibleEntries.map((entry) => (
          <div key={entry.id} className="studio-activity-row">
            <span className="studio-activity-kind">{entry.kind}</span>
            <span className="studio-activity-summary">
              {entry.summary || entry.widget_id || entry.surface_id}
            </span>
            <span className="studio-activity-time">
              {formatRelativeTime(entry.created_at)}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}

function NormalizedWidgetView({
  widget,
  compact = false,
}: {
  widget: NormalizedWidget;
  compact?: boolean;
}) {
  if (widget.kind === "checklist") {
    return (
      <ul className="studio-checklist">
        {(widget.checklist ?? []).map((item) => (
          <li key={item.id || item.label}>
            <span
              className={`studio-check-mark${item.checked ? " is-checked" : ""}`}
              aria-hidden="true"
            >
              {item.checked ? "x" : ""}
            </span>
            <span className="studio-check-label">{item.label}</span>
          </li>
        ))}
      </ul>
    );
  }
  if (widget.kind === "table" && widget.table) {
    if (compact) {
      const [primary, ...rest] = widget.table.columns;
      return (
        <div className="studio-table-summary">
          {widget.table.rows.length === 0 && (
            <div className="studio-table-summary-empty">No rows.</div>
          )}
          {widget.table.rows.slice(0, 4).map((row, index) => {
            const secondary = rest
              .map((column) => row[column.key])
              .filter(Boolean)
              .join(" / ");
            return (
              <div
                key={`${index}-${widget.table!.columns.map((c) => row[c.key]).join("|")}`}
                className="studio-table-summary-row"
              >
                <span>{primary ? row[primary.key] : `Row ${index + 1}`}</span>
                {secondary && <em>{secondary}</em>}
              </div>
            );
          })}
        </div>
      );
    }
    return (
      <div className="studio-table-wrap">
        <table className="studio-table">
          <thead>
            <tr>
              {widget.table.columns.map((column) => (
                <th key={column.key}>{column.label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {widget.table.rows.length === 0 ? (
              <tr>
                <td colSpan={widget.table.columns.length} className="studio-table-empty">
                  No rows.
                </td>
              </tr>
            ) : (
              widget.table.rows.map((row, index) => (
                <tr
                  key={`${index}-${widget.table!.columns.map((c) => row[c.key]).join("|")}`}
                >
                  {widget.table?.columns.map((column) => (
                    <td key={column.key}>{row[column.key]}</td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    );
  }
  return <div className="studio-markdown">{widget.markdown}</div>;
}

function Inspector({ record }: { record: SurfaceWidgetRecord | null }) {
  if (!record) {
    return (
      <StudioEmptyState
        icon={<Page />}
        title="No widget selected."
        copy="Select an artifact to see what it rendered and what wrote it."
      />
    );
  }
  return (
    <>
      <div className="studio-inspector-head">
        <div>
          <h2>{record.widget.title}</h2>
          <span>{record.widget.id}</span>
        </div>
        <span className="badge badge-neutral">{record.widget.kind}</span>
      </div>
      {record.widget.description && (
        <p className="studio-inspector-description">{record.widget.description}</p>
      )}

      {record.render.errors && record.render.errors.length > 0 && (
        <div className="studio-render-errors">
          {record.render.errors.map((error) => (
            <div key={error}>{error}</div>
          ))}
        </div>
      )}

      <section className="studio-inspector-section">
        <div className="studio-section-heading">
          <Eye />
          <h3>Rendered preview</h3>
        </div>
        <pre className="studio-preview">{record.render.preview_text || ""}</pre>
      </section>

      <section className="studio-inspector-section">
        <div className="studio-section-heading">
          <Code />
          <h3>Source</h3>
        </div>
        <div className="studio-source">
          {record.source_lines.map((line) => (
            <div key={line.number} className="studio-source-line">
              <span>{line.number}</span>
              <code>{line.text || " "}</code>
            </div>
          ))}
        </div>
      </section>
    </>
  );
}
