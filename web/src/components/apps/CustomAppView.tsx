import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EditPencil, MoreHoriz } from "iconoir-react";

import {
  deleteApp,
  getApp,
  listAppVersions,
  rollbackApp,
} from "../../api/apps";
import { navigateToSidebarApp } from "../../lib/sidebarNav";
import { useAppStore } from "../../stores/app";
import { confirm } from "../ui/ConfirmDialog";
import { showNotice } from "../ui/Toast";
import { CustomAppFrame } from "./CustomAppFrame";

interface CustomAppViewProps {
  appId: string;
}

/**
 * CustomAppView renders one agent-generated internal tool: a header (icon, name,
 * version, Edit, an overflow menu) above the sandboxed CustomAppFrame. Edit is
 * the primary action. The destructive Delete and the version rollback both live
 * in the overflow menu so an operator can't lose or break a depended-on tool
 * with a stray click — the trust net the modify wedge needs.
 */
export function CustomAppView({ appId }: CustomAppViewProps) {
  const queryClient = useQueryClient();
  const openUpdateAppDialog = useAppStore((s) => s.openUpdateAppDialog);
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["app", appId],
    queryFn: () => getApp(appId),
    refetchInterval: 10_000,
  });

  const versions = useQuery({
    queryKey: ["app-versions", appId],
    queryFn: () => listAppVersions(appId),
    enabled: menuOpen,
  });

  const rollback = useMutation({
    mutationFn: (version: number) => rollbackApp(appId, version),
    onSuccess: async (restored) => {
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
  const priorVersions = (versions.data ?? []).filter((v) => v !== app.version);

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
        <span
          className="custom-app-view__version"
          title={`Updated ${app.updatedAt}`}
        >
          v{app.version}
        </span>
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
              <div className="custom-app-view__menu-section">
                Restore a previous version
              </div>
              {versions.isLoading ? (
                <div className="custom-app-view__menu-empty">Loading…</div>
              ) : priorVersions.length === 0 ? (
                <div className="custom-app-view__menu-empty">
                  No earlier versions yet.
                </div>
              ) : (
                priorVersions.slice(0, 8).map((v) => (
                  <button
                    key={v}
                    type="button"
                    role="menuitem"
                    className="custom-app-view__menu-item"
                    disabled={rollback.isPending}
                    onClick={() => {
                      setMenuOpen(false);
                      rollback.mutate(v);
                    }}
                  >
                    Restore v{v}
                  </button>
                ))
              )}
              <div className="custom-app-view__menu-divider" />
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
      <div className="custom-app-view__body">
        <CustomAppFrame html={html} title={app.name} />
      </div>
    </div>
  );
}
