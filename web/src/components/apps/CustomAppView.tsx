import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ClockRotateRight,
  CursorPointer,
  EditPencil,
  MoreHoriz,
  Xmark,
} from "iconoir-react";

import {
  deleteApp,
  getApp,
  getAppVersion,
  listAppVersions,
  rollbackApp,
} from "../../api/apps";
import { formatRelativeTime } from "../../lib/format";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { useAppStore } from "../../stores/app";
import { confirm } from "../ui/ConfirmDialog";
import { showNotice } from "../ui/Toast";
import { AppLivePreview } from "./AppLivePreview";
import { AppVersionTimeline } from "./AppVersionTimeline";
import { type AppSelectPayload, CustomAppFrame } from "./CustomAppFrame";

interface CustomAppViewProps {
  appId: string;
}

/**
 * Build the App Builder description seed from a "select to edit" click: a
 * concise instruction stub naming the element + its source location, leaving a
 * trailing space so the human types only the actual change. Pure for tests.
 */
export function buildSelectSeed(sel: AppSelectPayload): string {
  const where = `${sel.file}:${sel.line}`;
  const label = sel.label ? ` ("${sel.label}")` : "";
  return `Change the <${sel.tag || "element"}> at \`${where}\`${label}: `;
}

/**
 * CustomAppView renders one agent-generated internal tool: a header (icon, name,
 * version, History, Edit, an overflow menu) above the sandboxed frame. Edit is
 * the primary action; the destructive Delete lives in the overflow menu so a
 * stray click can't remove a depended-on tool.
 *
 * History is the trust net behind the modify wedge: it opens an append-only
 * version timeline beside the preview. Selecting an older build previews it
 * NON-destructively (the current version is untouched); "Restore this version"
 * then re-publishes those bytes as a new forward version, so a restore is itself
 * reversible. An operator can look before they leap.
 */
export function CustomAppView({ appId }: CustomAppViewProps) {
  const queryClient = useQueryClient();
  const openUpdateAppDialog = useAppStore((s) => s.openUpdateAppDialog);
  const [menuOpen, setMenuOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  // The older build being previewed, or null while viewing the current build.
  const [previewVersion, setPreviewVersion] = useState<number | null>(null);
  // Live = the running dev server (HMR); Sealed = the published single-file
  // bundle. Default to Live so opening an app shows the real, current tool.
  const [mode, setMode] = useState<"live" | "sealed">("live");
  // "Select to edit" inspector toggle (dev/live only) + the latest runtime
  // error the app surfaced, shown as a dismissible banner over the preview.
  const [selectMode, setSelectMode] = useState(false);
  const [appError, setAppError] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["app", appId],
    queryFn: () => getApp(appId),
    refetchInterval: 10_000,
  });

  const versions = useQuery({
    queryKey: ["app-versions", appId],
    queryFn: () => listAppVersions(appId),
    enabled: historyOpen,
  });

  const currentVersion = data?.app.version ?? 0;
  const isPreviewing =
    previewVersion !== null && previewVersion !== currentVersion;

  const versionPreview = useQuery({
    queryKey: ["app-version", appId, previewVersion],
    queryFn: () => getAppVersion(appId, previewVersion as number),
    enabled: isPreviewing,
  });

  const rollback = useMutation({
    mutationFn: (version: number) => rollbackApp(appId, version),
    onSuccess: async (restored) => {
      setPreviewVersion(null);
      await queryClient.invalidateQueries({ queryKey: ["app", appId] });
      await queryClient.invalidateQueries({
        queryKey: ["app-versions", appId],
      });
      await queryClient.invalidateQueries({ queryKey: ["apps"] });
      showNotice(`Restored — now on v${restored.version}.`, "success");
    },
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Could not restore that version.",
        "error",
      );
    },
  });

  // Close the overflow menu on outside click.
  useEffect(() => {
    if (!menuOpen) return;
    function onDown(e: MouseEvent): void {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
      }
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [menuOpen]);

  function toggleHistory(): void {
    setHistoryOpen((open) => {
      // Leaving history drops any in-flight preview so the frame returns to live.
      if (open) setPreviewVersion(null);
      return !open;
    });
  }

  function onSelectVersion(version: number): void {
    // Selecting the current build is "back to live", not a preview.
    setPreviewVersion(version === currentVersion ? null : version);
  }

  function onSelectElement(sel: AppSelectPayload): void {
    // One-shot: a select ends the inspector mode, then opens the edit dialog
    // prefilled with a concise instruction stub from the clicked element.
    setSelectMode(false);
    if (data) {
      openUpdateAppDialog(data.app.id, data.app.name, buildSelectSeed(sel));
    }
  }

  function onDelete(): void {
    if (!data) return;
    setMenuOpen(false);
    confirm({
      title: "Delete app",
      message: `Delete "${data.app.name}"? This removes the tool for everyone in the office.`,
      danger: true,
      confirmLabel: "Delete",
      onConfirm: async () => {
        try {
          await deleteApp(appId);
          await queryClient.invalidateQueries({ queryKey: ["apps"] });
          showNotice(`Deleted ${data.app.name}.`, "success");
          navigateToSidebarApp("activity");
        } catch (err) {
          showNotice(
            err instanceof Error ? err.message : "Could not delete the app.",
            "error",
          );
        }
      },
    });
  }

  if (isLoading) {
    return (
      <div className="custom-app-view custom-app-view--state">
        <p className="custom-app-view__loading">Loading app…</p>
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="custom-app-view custom-app-view--state">
        <div className="custom-app-view__error" role="alert">
          <p>Could not load this app.</p>
          <p className="custom-app-view__error-detail">
            {error instanceof Error ? error.message : "Unknown error"}
          </p>
          <button
            type="button"
            className="custom-app-view__action"
            onClick={() => void refetch()}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  const { app, html } = data;

  return (
    <div className="custom-app-view">
      <header className="custom-app-view__header">
        <span className="custom-app-view__icon" aria-hidden="true">
          {app.icon || "🧩"}
        </span>
        <div className="custom-app-view__heading">
          <h1 className="custom-app-view__name">{app.name}</h1>
          {app.summary ? (
            <p className="custom-app-view__summary">{app.summary}</p>
          ) : null}
        </div>
        {/* The Live/Sealed toggle is meaningless while previewing an older,
            already-sealed build, so it yields to the preview banner. */}
        {isPreviewing ? null : (
          <div className="custom-app-view__mode">
            <button
              type="button"
              className="custom-app-view__mode-btn"
              aria-pressed={mode === "live"}
              onClick={() => setMode("live")}
            >
              Live
            </button>
            <button
              type="button"
              className="custom-app-view__mode-btn"
              aria-pressed={mode === "sealed"}
              onClick={() => setMode("sealed")}
            >
              Sealed
            </button>
          </div>
        )}
        <span
          className="custom-app-view__version"
          title={`Updated ${app.updatedAt}`}
        >
          v{app.version}
        </span>
        {/* Select to edit is a live-preview affordance: it intercepts a click
            in the running app to seed a precise edit. It's meaningless while
            viewing the sealed bundle or a read-only past version. */}
        {!isPreviewing && mode === "live" ? (
          <button
            type="button"
            className="custom-app-view__action"
            aria-pressed={selectMode}
            onClick={() => setSelectMode((on) => !on)}
            title="Click an element in the preview to edit it"
          >
            <CursorPointer width={15} height={15} />
            <span>Select to edit</span>
          </button>
        ) : null}
        <button
          type="button"
          className="custom-app-view__action"
          aria-pressed={historyOpen}
          onClick={toggleHistory}
        >
          <ClockRotateRight width={15} height={15} />
          <span>History</span>
        </button>
        <button
          type="button"
          className="custom-app-view__action"
          onClick={() => openUpdateAppDialog(app.id, app.name)}
        >
          <EditPencil width={15} height={15} />
          <span>Edit</span>
        </button>
        <div className="custom-app-view__menu-wrap" ref={menuRef}>
          <button
            type="button"
            className="custom-app-view__icon-btn"
            aria-label="More actions"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((open) => !open)}
          >
            <MoreHoriz width={16} height={16} />
          </button>
          {menuOpen ? (
            <div className="custom-app-view__menu" role="menu">
              <button
                type="button"
                role="menuitem"
                className="custom-app-view__menu-item custom-app-view__menu-item--danger"
                onClick={onDelete}
              >
                Delete app
              </button>
            </div>
          ) : null}
        </div>
      </header>
      <div
        className={`custom-app-view__body${
          historyOpen ? " custom-app-view__body--history" : ""
        }`}
      >
        {historyOpen ? (
          <AppVersionTimeline
            versions={versions.data ?? []}
            isLoading={versions.isLoading}
            selectedVersion={previewVersion}
            currentVersion={app.version}
            onSelect={onSelectVersion}
          />
        ) : null}
        <div className="custom-app-view__stage">
          {appError && !isPreviewing && mode === "live" ? (
            <div className="custom-app-view__app-error" role="alert">
              <span className="custom-app-view__app-error-text">
                {appError}
              </span>
              <button
                type="button"
                className="custom-app-view__app-error-dismiss"
                aria-label="Dismiss error"
                onClick={() => setAppError(null)}
              >
                <Xmark width={14} height={14} />
              </button>
            </div>
          ) : null}
          {isPreviewing ? (
            <PreviewBanner
              version={previewVersion as number}
              currentVersion={app.version}
              updatedBy={versionPreview.data?.updatedBy}
              updatedAt={versionPreview.data?.updatedAt}
              restoring={rollback.isPending}
              onRestore={() => rollback.mutate(previewVersion as number)}
              onBack={() => setPreviewVersion(null)}
            />
          ) : null}
          {isPreviewing ? (
            versionPreview.isLoading || !versionPreview.data ? (
              <div className="custom-app-view__preview-loading">
                Loading v{previewVersion}…
              </div>
            ) : (
              <CustomAppFrame
                html={versionPreview.data.html}
                title={`${app.name} (v${previewVersion})`}
              />
            )
          ) : mode === "live" ? (
            <AppLivePreview
              appId={appId}
              title={app.name}
              selectMode={selectMode}
              onSelect={onSelectElement}
              onAppError={(err) =>
                setAppError(err.message || "The app threw a runtime error.")
              }
            />
          ) : (
            <CustomAppFrame html={html} title={app.name} />
          )}
        </div>
      </div>
    </div>
  );
}

interface PreviewBannerProps {
  version: number;
  currentVersion: number;
  updatedBy?: string;
  updatedAt?: string;
  restoring: boolean;
  onRestore: () => void;
  onBack: () => void;
}

/**
 * PreviewBanner sits atop a non-destructive version preview: it names what the
 * operator is looking at and offers the two safe exits — restore these bytes as
 * a new forward version, or step back to the current build.
 */
function PreviewBanner({
  version,
  currentVersion,
  updatedBy,
  updatedAt,
  restoring,
  onRestore,
  onBack,
}: PreviewBannerProps) {
  const meta = [updatedBy, updatedAt && formatRelativeTime(updatedAt)]
    .filter(Boolean)
    .join(" · ");
  return (
    <div className="custom-app-view__banner" role="status">
      <span className="custom-app-view__banner-text">
        Viewing <strong>v{version}</strong> (read-only)
        {meta ? ` · ${meta}` : ""}
      </span>
      <div className="custom-app-view__banner-actions">
        <button
          type="button"
          className="custom-app-view__banner-btn custom-app-view__banner-btn--primary"
          disabled={restoring}
          onClick={onRestore}
        >
          {restoring ? "Restoring…" : "Restore this version"}
        </button>
        <button
          type="button"
          className="custom-app-view__banner-btn"
          onClick={onBack}
        >
          Back to current v{currentVersion}
        </button>
      </div>
    </div>
  );
}
