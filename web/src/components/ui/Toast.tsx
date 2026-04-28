import { useCallback, useEffect, useState } from "react";

interface ToastOptions {
  actionLabel?: string;
  onAction?: () => void;
  /** ms — total time the toast stays visible. Defaults to 4000. */
  persist?: number;
}

interface ToastItem {
  id: number;
  message: string;
  type: "success" | "error" | "info";
  actionLabel?: string;
  onAction?: () => void;
  persist: number;
}

let toastId = 0;
let addToastFn:
  | ((msg: string, type: ToastItem["type"], options?: ToastOptions) => void)
  | null = null;

const DEFAULT_TOAST_MS = 4000;

/** Global toast trigger — call from anywhere (matches legacy showNotice API). */
export function showNotice(message: string, type: ToastItem["type"] = "info") {
  addToastFn?.(message, type);
}

/**
 * Show a toast with an inline Undo button. Auto-dismisses after `ms`
 * (default 5000). Calling `onUndo` triggers the action and closes the toast.
 */
export function showUndoToast(
  message: string,
  onUndo: () => void,
  ms = 5000,
): void {
  addToastFn?.(message, "info", {
    actionLabel: "Undo",
    onAction: onUndo,
    persist: ms,
  });
}

export function ToastContainer() {
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const addToast = useCallback(
    (message: string, type: ToastItem["type"], options?: ToastOptions) => {
      const id = ++toastId;
      const persist = options?.persist ?? DEFAULT_TOAST_MS;
      const item: ToastItem = {
        id,
        message,
        type,
        actionLabel: options?.actionLabel,
        onAction: options?.onAction,
        persist,
      };
      setToasts((prev) => [...prev, item]);
      setTimeout(() => {
        setToasts((prev) => prev.filter((t) => t.id !== id));
      }, persist);
    },
    [],
  );

  useEffect(() => {
    addToastFn = addToast;
    return () => {
      addToastFn = null;
    };
  }, [addToast]);

  if (toasts.length === 0) return null;

  return (
    <div
      style={{
        position: "fixed",
        bottom: 20,
        right: 20,
        zIndex: 300,
        display: "flex",
        flexDirection: "column",
        gap: 8,
        pointerEvents: "none",
      }}
    >
      {toasts.map((t) => (
        <ToastRow key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
      ))}
    </div>
  );
}

interface ToastRowProps {
  toast: ToastItem;
  onDismiss: () => void;
}

function ToastRow({ toast, onDismiss }: ToastRowProps) {
  const background =
    toast.type === "error"
      ? "var(--red)"
      : toast.type === "success"
        ? "var(--green)"
        : "var(--accent)";

  const handleAction = (e: React.MouseEvent) => {
    e.stopPropagation();
    toast.onAction?.();
    onDismiss();
  };

  return (
    <div
      className="animate-fade"
      style={{
        padding: "10px 16px",
        borderRadius: "var(--radius-md)",
        fontSize: 13,
        fontWeight: 500,
        fontFamily: "var(--font-sans)",
        boxShadow: "0 4px 12px rgba(0,0,0,0.15)",
        pointerEvents: "auto",
        cursor: "pointer",
        maxWidth: 360,
        background,
        color: "white",
        display: "flex",
        alignItems: "center",
        gap: 12,
      }}
      onClick={onDismiss}
    >
      <span style={{ flex: 1 }}>{toast.message}</span>
      {toast.actionLabel ? (
        <button
          type="button"
          onClick={handleAction}
          style={{
            background: "rgba(255,255,255,0.18)",
            color: "white",
            border: "1px solid rgba(255,255,255,0.4)",
            padding: "4px 10px",
            borderRadius: 4,
            fontSize: 12,
            fontWeight: 600,
            cursor: "pointer",
            fontFamily: "inherit",
            whiteSpace: "nowrap",
          }}
        >
          {toast.actionLabel}
        </button>
      ) : null}
    </div>
  );
}
