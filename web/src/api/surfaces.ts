import { get, patch, post, sseURL } from "./client";

export interface SurfaceManifest {
  id: string;
  title: string;
  channel: string;
  owner?: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
  widget_count?: number;
}

export interface SurfaceHistoryEntry {
  id: string;
  surface_id: string;
  widget_id?: string;
  kind: string;
  actor?: string;
  summary?: string;
  created_at: string;
}

export interface NumberedLine {
  number: number;
  text: string;
}

export interface NormalizedWidget {
  id: string;
  title: string;
  description?: string;
  kind: "checklist" | "table" | "markdown" | string;
  schema_version: string;
  checklist?: Array<{ id?: string; label: string; checked: boolean }>;
  table?: {
    columns: Array<{ key: string; label: string }>;
    rows: Array<Record<string, string>>;
  };
  markdown?: string;
}

export interface WidgetRenderResult {
  schema_ok: boolean;
  render_ok: boolean;
  normalized_widget?: NormalizedWidget;
  preview_text?: string;
  errors?: string[];
}

export interface SurfaceWidget {
  id: string;
  title: string;
  description?: string;
  kind: string;
  schema_version?: string;
  source: string;
  created_by?: string;
  updated_by?: string;
  created_at?: string;
  updated_at?: string;
}

export interface SurfaceWidgetRecord {
  widget: SurfaceWidget;
  source_lines: NumberedLine[];
  render: WidgetRenderResult;
}

export interface SurfaceDetail {
  surface: SurfaceManifest;
  widgets: SurfaceWidgetRecord[];
  history?: SurfaceHistoryEntry[];
}

export interface SurfaceEvent {
  type: string;
  surface_id: string;
  widget_id?: string;
  channel: string;
  actor?: string;
  title?: string;
  created_at: string;
}

export function listSurfaces() {
  return get<{ surfaces: SurfaceManifest[] }>("/surfaces", {
    viewer_slug: "human",
  });
}

export function readSurface(surfaceId: string) {
  return get<SurfaceDetail>(`/surfaces/${encodeURIComponent(surfaceId)}`, {
    viewer_slug: "human",
  });
}

export function createSurface(input: {
  title: string;
  channel: string;
  id?: string;
}) {
  return post<{ surface: SurfaceManifest }>("/surfaces", {
    ...input,
    created_by: "human",
    my_slug: "human",
  });
}

export function upsertWidget(surfaceId: string, widget: Partial<SurfaceWidget>) {
  return post<SurfaceWidgetRecord>(
    `/surfaces/${encodeURIComponent(surfaceId)}/widgets`,
    {
      my_slug: "human",
      actor: "human",
      widget,
    },
  );
}

export function patchWidget(
  surfaceId: string,
  widgetId: string,
  patchBody: {
    mode?: "line" | "snippet";
    start_line?: number;
    end_line?: number;
    search?: string;
    replacement: string;
  },
) {
  return patch<SurfaceWidgetRecord>(
    `/surfaces/${encodeURIComponent(surfaceId)}/widgets/${encodeURIComponent(widgetId)}`,
    { ...patchBody, actor: "human" },
  );
}

export function renderCheck(
  surfaceId: string,
  widgetId: string,
  widget?: Partial<SurfaceWidget>,
) {
  return post<WidgetRenderResult>(
    `/surfaces/${encodeURIComponent(surfaceId)}/widgets/${encodeURIComponent(widgetId)}/render-check`,
    {
      my_slug: "human",
      actor: "human",
      widget,
    },
  );
}

export function subscribeSurfaceEvents(handler: (event: SurfaceEvent) => void) {
  const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
  if (!ES) return () => {};
  const source = new ES(sseURL("/events"));
  const names = [
    "surface:created",
    "surface:updated",
    "surface:deleted",
    "surface:widget_created",
    "surface:widget_updated",
    "surface:render_checked",
  ];
  const listeners = names.map((name) => {
    const listener = (event: Event) => {
      if (!("data" in event) || typeof event.data !== "string") return;
      try {
        handler(JSON.parse(event.data) as SurfaceEvent);
      } catch {
        // Ignore malformed event payloads. EventSource will keep reconnecting.
      }
    };
    source.addEventListener(name, listener);
    return [name, listener] as const;
  });
  return () => {
    for (const [name, listener] of listeners) {
      source.removeEventListener(name, listener);
    }
    source.close();
  };
}
