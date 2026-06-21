import { type RefObject, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ClockRotateRight,
  CursorPointer,
  EditPencil,
  MoreHoriz,
  Xmark,
} from "iconoir-react";

import {
  type CustomApp,
  type CustomAppDetail,
  type CustomAppVersion,
  deleteApp,
  getApp,
  getAppVersion,
  listAppVersions,
  openAppEditSession,
  rollbackApp,
} from "../../api/apps";
import { formatRelativeTime } from "../../lib/format";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { useAppStore } from "../../stores/app";
import { confirm } from "../ui/ConfirmDialog";
import { showNotice } from "../ui/Toast";
import { AppEditPanel } from "./AppEditPanel";
import { AppLivePreview } from "./AppLivePreview";
import { AppVersionTimeline } from "./AppVersionTimeline";
import { type AppSelectPayload, CustomAppFrame } from "./CustomAppFrame";

interface CustomAppViewProps {
  appId: string;
}

/**
 * Build the App Builder edit instruction from a "select to edit" click: a
 * concise instruction stub naming the element + its source location, leaving a
 * trailing space so the human types only the actual change. Pure for tests.
 *
 * The element text/tag/file are app-supplied (a hostile app could forge them),
 * so strip backticks and newlines before interpolating: that keeps a crafted
 * label from breaking out of the code-span or injecting extra lines into the
 * seed the human reviews. The human still edits + sends from the composer, so
 * this is defense-in-depth, not the trust boundary.
 */
export function buildSelectSeed(sel: AppSelectPayload): string {
  const clean = (s: string): string => s.replace(/[`\r\n]+/g, " ").trim();
  const where = `${clean(sel.file)}:${sel.line}`;
  const label = sel.label ? ` ("${clean(sel.label)}")` : "";
  const tag = clean(sel.tag) || "element";
  return `Change the <${tag}> at \`${where}\`${label}: `;
}

/**
 * CustomAppView renders one agent-generated internal tool: a header (icon, name,
 * version, Select-to-edit, History, Edit, an overflow menu) above the app
 * surface. There is ONE app surface, not a mode toggle: a finished app shows its
 * published (sealed) bundle. The destructive Delete lives in the overflow menu
 * so a stray click can't remove a depended-on tool.
 *
 * Edit is the modify wedge: it opens a persistent, per-app chat with the App
 * Builder in a right-side panel (v0/Cursor-style). While that panel is open the
 * main stage switches to the LIVE dev-server preview so the human watches edits
 * hot-reload as the agent republishes; closing the panel returns to the sealed
 * bundle. "Select to edit" is the same wedge primed with a precise element ref.
 *
 * History is the trust net behind editing: it opens an append-only version
 * timeline beside the surface. Selecting an older build previews it
 * NON-destructively (the current version is untouched); "Restore this version"
 * then re-publishes those bytes as a new forward version, so a restore is itself
 * reversible. An operator can look before they leap.
 *
 * This component is the orchestrator (queries, mutations, state, handlers); the
 * header and body chrome live in AppViewHeader / AppViewBody below.
 */
export function CustomAppView({ appId }: CustomAppViewProps) {
  const queryClient = useQueryClient();
  const setPendingComposerDraft = useAppStore((s) => s.setPendingComposerDraft);
  const [menuOpen, setMenuOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  // The per-app edit chat side panel — a persistent conversation with the App
  // Builder for THIS app. Opening it switches the stage to the live preview so
  // republished changes hot-reload in front of the human.
  const [editOpen, setEditOpen] = useState(false);
  // The older build being previewed, or null while viewing the current build.
  const [previewVersion, setPreviewVersion] = useState<number | null>(null);
  // "Select to edit" inspector toggle (live preview only) + the latest runtime
  // error the app surfaced, shown as a dismissible banner over the preview.
  const [selectMode, setSelectMode] = useState(false);
  const [appError, setAppError] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["app", appId],
    queryFn: () => getApp(appId),
    // The published bundle only changes on a republish (rare), which the edit
    // flow already invalidates. Don't refetch on every window focus — the
    // global default is on, and for a static app it just churns the query each
    // time the browser tab regains focus for no benefit.
    refetchOnWindowFocus: false,
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

  // Opening the edit chat on an app with no bound thread (a legacy app, or one
  // registered html-only) lazily mints one, then opens the panel. We seed the
  // returned channel straight into the cached app so the panel mounts without a
  // refetch round-trip.
  const ensureEditSession = useMutation({
    mutationFn: () => openAppEditSession(appId),
    onSuccess: (channel) => {
      queryClient.setQueryData<CustomAppDetail>(["app", appId], (prev) =>
        prev ? { ...prev, app: { ...prev.app, editChannel: channel } } : prev,
      );
      setEditOpen(true);
    },
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Could not open the edit chat.",
        "error",
      );
    },
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
      // Leaving history drops any in-flight preview so the frame returns to the
      // current surface.
      if (open) setPreviewVersion(null);
      return !open;
    });
  }

  function onSelectVersion(version: number): void {
    // Selecting the current build is "back to current", not a preview.
    setPreviewVersion(version === currentVersion ? null : version);
  }

  function onToggleEdit(): void {
    if (editOpen) {
      // Leaving edit also drops select mode (a live-preview-only affordance).
      setSelectMode(false);
      setEditOpen(false);
      return;
    }
    // An already-bound app opens immediately; a legacy app with no thread mints
    // one first (the mutation opens the panel on success).
    if (data?.app.editChannel) {
      setEditOpen(true);
      return;
    }
    ensureEditSession.mutate();
  }

  function onToggleSelect(): void {
    // "Select to edit" is a first-class entry point — it works whether or not
    // the edit chat is already open. The inspector lives in the live preview,
    // which only mounts when the panel is open, so enabling select also opens
    // the panel ("if the panel is not open, open it"). Disabling just ends the
    // inspector and leaves the panel as-is.
    setSelectMode((on) => {
      const next = !on;
      if (next) setEditOpen(true);
      return next;
    });
  }

  function onSelectElement(sel: AppSelectPayload): void {
    // One-shot: a select ends the inspector, opens the edit chat (if it wasn't
    // already), and seeds the composer with the clicked element's details as
    // context. The human edits + sends it as a normal message in the per-app
    // edit thread.
    setSelectMode(false);
    if (data?.app.editChannel) {
      setEditOpen(true);
      setPendingComposerDraft(data.app.editChannel, buildSelectSeed(sel));
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
  // Every app is editable: Edit is always offered. An app with no bound thread
  // (registered html-only, or minted before the edit-channel field existed)
  // mints one lazily on the first click (ensureEditSession). `canEdit` only
  // gates the live-preview-only "Select to edit" affordance, which needs the
  // bound channel that's guaranteed to exist by the time the panel is open.
  const canEdit = Boolean(app.editChannel);

  return (
    <div className="custom-app-view">
      <AppViewHeader
        app={app}
        isPreviewing={isPreviewing}
        canEdit={canEdit}
        editPending={ensureEditSession.isPending}
        editOpen={editOpen}
        selectMode={selectMode}
        onToggleSelect={onToggleSelect}
        historyOpen={historyOpen}
        onToggleHistory={toggleHistory}
        onToggleEdit={onToggleEdit}
        menuOpen={menuOpen}
        onToggleMenu={() => setMenuOpen((open) => !open)}
        menuRef={menuRef}
        onDelete={onDelete}
      />
      <AppViewBody
        appId={appId}
        app={app}
        html={html}
        editOpen={editOpen && canEdit}
        onCloseEdit={() => {
          setEditOpen(false);
          setSelectMode(false);
        }}
        historyOpen={historyOpen}
        versions={versions.data ?? []}
        versionsLoading={versions.isLoading}
        previewVersion={previewVersion}
        isPreviewing={isPreviewing}
        previewHtml={versionPreview.data?.html}
        previewLoading={versionPreview.isLoading}
        previewUpdatedBy={versionPreview.data?.updatedBy}
        previewUpdatedAt={versionPreview.data?.updatedAt}
        restoring={rollback.isPending}
        onRestore={() => rollback.mutate(previewVersion as number)}
        onBack={() => setPreviewVersion(null)}
        onSelectVersion={onSelectVersion}
        selectMode={selectMode}
        onSelectElement={onSelectElement}
        onAppError={(msg) => setAppError(msg)}
        appError={appError}
        onDismissError={() => setAppError(null)}
      />
    </div>
  );
}

interface AppViewHeaderProps {
  app: CustomApp;
  isPreviewing: boolean;
  canEdit: boolean;
  editPending: boolean;
  editOpen: boolean;
  selectMode: boolean;
  onToggleSelect: () => void;
  historyOpen: boolean;
  onToggleHistory: () => void;
  onToggleEdit: () => void;
  menuOpen: boolean;
  onToggleMenu: () => void;
  menuRef: RefObject<HTMLDivElement | null>;
  onDelete: () => void;
}

function AppViewHeader({
  app,
  isPreviewing,
  canEdit,
  editPending,
  editOpen,
  selectMode,
  onToggleSelect,
  historyOpen,
  onToggleHistory,
  onToggleEdit,
  menuOpen,
  onToggleMenu,
  menuRef,
  onDelete,
}: AppViewHeaderProps) {
  return (
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
      <span
        className="custom-app-view__version"
        title={`Updated ${app.updatedAt}`}
      >
        v{app.version}
      </span>
      {/* Select to edit primes the edit chat with a precise element ref. It's a
          first-class entry point — offered whenever the app is editable and not
          showing a read-only past version. Toggling it on opens the edit panel
          (live preview + inspector) if it isn't already. */}
      {canEdit && !isPreviewing ? (
        <button
          type="button"
          className="custom-app-view__action"
          aria-pressed={selectMode}
          onClick={onToggleSelect}
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
        onClick={onToggleHistory}
      >
        <ClockRotateRight width={15} height={15} />
        <span>History</span>
      </button>
      <button
        type="button"
        className="custom-app-view__action"
        aria-pressed={editOpen}
        disabled={editPending}
        onClick={onToggleEdit}
      >
        <EditPencil width={15} height={15} />
        <span>{editPending ? "Opening…" : "Edit"}</span>
      </button>
      <div className="custom-app-view__menu-wrap" ref={menuRef}>
        <button
          type="button"
          className="custom-app-view__icon-btn"
          aria-label="More actions"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={onToggleMenu}
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
  );
}

interface AppViewBodyProps {
  appId: string;
  app: CustomApp;
  html: string;
  editOpen: boolean;
  onCloseEdit: () => void;
  historyOpen: boolean;
  versions: CustomAppVersion[];
  versionsLoading: boolean;
  previewVersion: number | null;
  isPreviewing: boolean;
  previewHtml?: string;
  previewLoading: boolean;
  previewUpdatedBy?: string;
  previewUpdatedAt?: string;
  restoring: boolean;
  onRestore: () => void;
  onBack: () => void;
  onSelectVersion: (version: number) => void;
  selectMode: boolean;
  onSelectElement: (sel: AppSelectPayload) => void;
  onAppError: (message: string) => void;
  appError: string | null;
  onDismissError: () => void;
}

function AppViewBody({
  appId,
  app,
  html,
  editOpen,
  onCloseEdit,
  historyOpen,
  versions,
  versionsLoading,
  previewVersion,
  isPreviewing,
  previewHtml,
  previewLoading,
  previewUpdatedBy,
  previewUpdatedAt,
  restoring,
  onRestore,
  onBack,
  onSelectVersion,
  selectMode,
  onSelectElement,
  onAppError,
  appError,
  onDismissError,
}: AppViewBodyProps) {
  const className = [
    "custom-app-view__body",
    historyOpen ? "custom-app-view__body--history" : "",
    editOpen ? "custom-app-view__body--edit" : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <div className={className}>
      {historyOpen ? (
        <AppVersionTimeline
          versions={versions}
          isLoading={versionsLoading}
          selectedVersion={previewVersion}
          currentVersion={app.version}
          onSelect={onSelectVersion}
        />
      ) : null}
      <div className="custom-app-view__stage">
        {appError && editOpen && !isPreviewing ? (
          <div className="custom-app-view__app-error" role="alert">
            <span className="custom-app-view__app-error-text">{appError}</span>
            <button
              type="button"
              className="custom-app-view__app-error-dismiss"
              aria-label="Dismiss error"
              onClick={onDismissError}
            >
              <Xmark width={14} height={14} />
            </button>
          </div>
        ) : null}
        {isPreviewing ? (
          <PreviewBanner
            version={previewVersion as number}
            currentVersion={app.version}
            updatedBy={previewUpdatedBy}
            updatedAt={previewUpdatedAt}
            restoring={restoring}
            onRestore={onRestore}
            onBack={onBack}
          />
        ) : null}
        <AppViewStageFrame
          appId={appId}
          app={app}
          html={html}
          editOpen={editOpen}
          isPreviewing={isPreviewing}
          previewVersion={previewVersion}
          previewHtml={previewHtml}
          previewLoading={previewLoading}
          selectMode={selectMode}
          onSelectElement={onSelectElement}
          onAppError={onAppError}
        />
      </div>
      {editOpen && app.editChannel ? (
        <AppEditPanel
          appName={app.name}
          channel={app.editChannel}
          onClose={onCloseEdit}
        />
      ) : null}
    </div>
  );
}

interface AppViewStageFrameProps {
  appId: string;
  app: CustomApp;
  html: string;
  editOpen: boolean;
  isPreviewing: boolean;
  previewVersion: number | null;
  previewHtml?: string;
  previewLoading: boolean;
  selectMode: boolean;
  onSelectElement: (sel: AppSelectPayload) => void;
  onAppError: (message: string) => void;
}

/**
 * AppViewStageFrame picks the right surface for the stage: a read-only snapshot
 * of a past version (with a loading shim); the live dev preview while the edit
 * chat is open (so republished changes hot-reload in front of the human); or the
 * sealed single-file bundle (the default, finished app).
 */
function AppViewStageFrame({
  appId,
  app,
  html,
  editOpen,
  isPreviewing,
  previewVersion,
  previewHtml,
  previewLoading,
  selectMode,
  onSelectElement,
  onAppError,
}: AppViewStageFrameProps) {
  if (isPreviewing) {
    if (previewLoading || previewHtml === undefined) {
      return (
        <div className="custom-app-view__preview-loading">
          Loading v{previewVersion}…
        </div>
      );
    }
    return (
      <CustomAppFrame
        appId={appId}
        html={previewHtml}
        title={`${app.name} (v${previewVersion})`}
      />
    );
  }
  if (editOpen) {
    return (
      <AppLivePreview
        appId={appId}
        title={app.name}
        selectMode={selectMode}
        onSelect={onSelectElement}
        onAppError={(err) =>
          onAppError(err.message || "The app threw a runtime error.")
        }
      />
    );
  }
  return <CustomAppFrame appId={appId} html={html} title={app.name} />;
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
