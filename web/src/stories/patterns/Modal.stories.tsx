import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Xmark } from "iconoir-react";

const meta: Meta = {
  title: "Patterns/Modal shell",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const Default: StoryObj = {
  render: () => {
    function Wrapper() {
      const [open, setOpen] = useState(true);
      return (
        <div style={{ minHeight: 480, padding: 24 }}>
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => setOpen(true)}
            disabled={open}
          >
            Open modal
          </button>
          {open ? (
            <div className="modal-backdrop" onClick={() => setOpen(false)}>
              <div
                className="modal-shell"
                role="dialog"
                aria-modal="true"
                aria-labelledby="modal-shell-story-title"
                style={{ padding: 24 }}
                onClick={(e) => e.stopPropagation()}
              >
                <header
                  style={{
                    display: "flex",
                    alignItems: "flex-start",
                    justifyContent: "space-between",
                    gap: 12,
                    marginBottom: 12,
                  }}
                >
                  <div>
                    <h2
                      id="modal-shell-story-title"
                      style={{
                        margin: 0,
                        fontSize: 17,
                        fontWeight: 600,
                        color: "var(--text)",
                      }}
                    >
                      Delete workspace?
                    </h2>
                    <p
                      style={{
                        margin: "4px 0 0",
                        color: "var(--text-secondary)",
                        fontSize: 13,
                      }}
                    >
                      This permanently removes the workspace and its history.
                    </p>
                  </div>
                  <button
                    type="button"
                    className="icon-btn"
                    aria-label="Close"
                    onClick={() => setOpen(false)}
                  >
                    <Xmark width={18} height={18} />
                  </button>
                </header>
                <footer
                  style={{
                    display: "flex",
                    justifyContent: "flex-end",
                    gap: 8,
                    marginTop: 16,
                  }}
                >
                  <button
                    type="button"
                    className="btn btn-ghost"
                    onClick={() => setOpen(false)}
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    className="btn btn-danger"
                    onClick={() => setOpen(false)}
                  >
                    Delete
                  </button>
                </footer>
              </div>
            </div>
          ) : null}
        </div>
      );
    }
    return <Wrapper />;
  },
};

export const Guidance: StoryObj = {
  render: () => (
    <div
      style={{
        maxWidth: 720,
        padding: 24,
        color: "var(--text)",
        lineHeight: 1.6,
      }}
    >
      <h2 style={{ margin: 0, marginBottom: 8, fontSize: 18 }}>Modal shell</h2>
      <p style={{ margin: "0 0 16px", color: "var(--text-secondary)" }}>
        Pair <code>.modal-backdrop</code> with <code>.modal-shell</code> for
        every dialog. The backdrop owns the scrim + z-index; the shell owns the
        card geometry, max-width, and elevation. Don't compose them
        independently.
      </p>
      <pre
        style={{
          background: "var(--bg-warm)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-sm)",
          padding: 12,
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          overflowX: "auto",
        }}
      >{`<div className="modal-backdrop" onClick={onClose}>
  <div className="modal-shell" onClick={stopPropagation}>
    {children}
  </div>
</div>`}</pre>
      <ul
        style={{
          marginTop: 16,
          paddingLeft: 20,
          color: "var(--text-secondary)",
          fontSize: 14,
        }}
      >
        <li>
          z-index ladder: <code>--z-overlay</code> (default modals) →{" "}
          <code>--z-modal-critical</code> (must-not-be-dismissed) →{" "}
          <code>--z-toast</code>.
        </li>
        <li>
          Backdrop click dismisses by default — override
          <code>onClick</code> if the dialog is gated (typed phrase, payment).
        </li>
        <li>
          Use <code>SidePanel</code> (a right-aligned variant) for "view full"
          surfaces; modals are for decisions and confirmations.
        </li>
      </ul>
    </div>
  ),
};
