import { del, get, post } from "./client";
import type { Task, TaskResponse } from "./tasks";

/**
 * CustomApp is the manifest for an agent-generated internal tool. Mirrors the
 * Go CustomApp shape (internal/team/custom_app.go).
 */
export interface CustomApp {
  id: string;
  slug: string;
  name: string;
  icon: string;
  summary?: string;
  description?: string;
  entry: string;
  version: number;
  /**
   * "building" = a pre-scaffolded app awaiting its first published build
   * (shown as a building row, not a clickable app). Absent/"ready" = published.
   */
  status?: "building" | "ready";
  /**
   * Slug of the app's persistent edit thread — the channel of the App Builder
   * task that created/improves it (`task-<id>`). The app view binds its
   * "chat to edit" side panel to this channel: a human message there re-engages
   * the App Builder to read get_app + republish via register_app. Absent for
   * apps minted before this field existed or registered html-only (no task).
   */
  editChannel?: string;
  createdBy: string;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
  contentHash: string;
}

export interface CustomAppDetail {
  app: CustomApp;
  html: string;
}

export async function listApps(): Promise<CustomApp[]> {
  const res = await get<{ apps: CustomApp[] }>("/apps");
  return res.apps ?? [];
}

export async function getApp(id: string): Promise<CustomAppDetail> {
  return get<CustomAppDetail>(`/apps/${encodeURIComponent(id)}`);
}

export async function deleteApp(id: string): Promise<void> {
  await del(`/apps/${encodeURIComponent(id)}`);
}

/**
 * CustomAppVersion is one retained build in an app's append-only history.
 * Metadata (updatedBy/updatedAt) is captured at snapshot time; builds from
 * before that existed degrade to just the version number. Mirrors the Go
 * CustomAppVersion shape (internal/team/custom_app.go).
 */
export interface CustomAppVersion {
  version: number;
  updatedBy?: string;
  updatedAt?: string;
  /** True for the app's live current build. */
  current: boolean;
}

export async function listAppVersions(id: string): Promise<CustomAppVersion[]> {
  const res = await get<{ versions: CustomAppVersion[] }>(
    `/apps/${encodeURIComponent(id)}/versions`,
  );
  return res.versions ?? [];
}

export interface AppVersionDetail extends CustomAppVersion {
  html: string;
}

/**
 * getAppVersion reads one retained build's bytes + metadata for non-destructive
 * preview. It NEVER changes the current version — that is the separate
 * {@link rollbackApp}. The bytes render in the same sandboxed frame as the
 * sealed current view.
 */
export async function getAppVersion(
  id: string,
  version: number,
): Promise<AppVersionDetail> {
  return get<AppVersionDetail>(
    `/apps/${encodeURIComponent(id)}/versions/${version}`,
  );
}

/**
 * rollbackApp restores a prior version's bytes as a new forward version. History
 * is append-only, so a rollback is itself reversible — this is the trust net that
 * lets operators edit a depended-on tool without fear.
 */
export async function rollbackApp(
  id: string,
  version: number,
): Promise<CustomApp> {
  const res = await post<{ app: CustomApp }>(
    `/apps/${encodeURIComponent(id)}/rollback`,
    { version, actor: "human" },
  );
  return res.app;
}

/**
 * Live dev-server preview status (GET /apps/{id}/dev). The broker runs a real
 * Vite dev server per app behind a CSP-injecting proxy; `url` is the proxy
 * origin to load in the iframe once `ready`. Until then `boot_log` streams the
 * install/boot output. Mirrors the Go appDevStatus shape.
 */
export interface AppDevStatus {
  ready: boolean;
  url?: string;
  boot_log?: string;
  error?: string;
}

/** Ensure the app's live dev server is running and return its status. */
export async function ensureAppDev(id: string): Promise<AppDevStatus> {
  return get<AppDevStatus>(`/apps/${encodeURIComponent(id)}/dev`);
}

/** Poll the dev server's status without (re)starting it. */
export async function getAppDevStatus(id: string): Promise<AppDevStatus> {
  return get<AppDevStatus>(`/apps/${encodeURIComponent(id)}/dev/status`);
}

/** Tear down the app's live dev server. */
export async function stopAppDev(id: string): Promise<void> {
  await post(`/apps/${encodeURIComponent(id)}/dev/stop`, { actor: "human" });
}

export interface AppBuildRequest {
  name: string;
  description: string;
  icon?: string;
  summary?: string;
  /** Set when improving an existing app rather than creating a new one. */
  appId?: string;
}

/**
 * requestAppBuild kicks off an App Builder task for an explicit, human-initiated
 * build/improve (the /create-app, /update-app slash commands and the Edit button
 * on an app screen). These paths skip the propose_app approval gate because the
 * human already authorized the work. Implicit agent intent goes through
 * propose_app -> a non-blocking approval instead.
 *
 * Returns the created Task so the caller can open its chat — an App Builder
 * build is a normal task, and the human watches it happen in the task channel
 * (with the live preview) rather than being left with a fire-and-forget toast.
 */
export async function requestAppBuild(req: AppBuildRequest): Promise<Task> {
  const verb = req.appId ? "Improve" : "Build";
  const title = `${verb} app: ${req.name}`;
  const res = await post<TaskResponse>("/tasks", {
    action: "create",
    channel: "general",
    title,
    details: composeAppBrief(req),
    owner: "app-builder",
    created_by: "human",
    task_type: "issue",
  });
  return res.task;
}

function composeAppBrief(req: AppBuildRequest): string {
  const lines: string[] = [];
  if (req.appId) {
    lines.push(`Improve the existing app \`${req.appId}\` ("${req.name}").`);
    lines.push(
      "First call get_app to read its current source and manifest, then apply the change below.",
    );
  } else {
    lines.push(`Build a new internal tool named "${req.name}".`);
  }
  if (req.summary?.trim()) {
    lines.push("", `Summary: ${req.summary.trim()}`);
  }
  lines.push("", "What it should do:", req.description.trim());
  lines.push(
    "",
    `When the build passes, register it with register_app${
      req.appId ? ` (app_id=${req.appId})` : ""
    } so it appears under Apps.`,
  );
  return lines.join("\n");
}
