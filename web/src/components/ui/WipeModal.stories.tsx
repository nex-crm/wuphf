import { useEffect, useRef, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ShredWarningCopy } from "./ShredWarning";
import { showNotice, ToastContainer } from "./Toast";
import { WipeModal } from "./WipeModal";

const meta: Meta<typeof WipeModal> = {
  title: "Design System/Organisms/WipeModal",
  component: WipeModal,
  parameters: { layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof WipeModal>;

export const Critical: Story = {
  render: () => {
    function Wrapper() {
      const [open, setOpen] = useState(true);
      const [busy, setBusy] = useState(false);
      const shredTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
      useEffect(() => {
        return () => {
          if (shredTimerRef.current !== null) {
            clearTimeout(shredTimerRef.current);
            shredTimerRef.current = null;
          }
        };
      }, []);
      return (
        <div style={{ minHeight: 400, padding: 24 }}>
          <button
            type="button"
            className="btn"
            onClick={() => setOpen(true)}
            disabled={open}
          >
            Open wipe modal
          </button>
          {open ? (
            <WipeModal
              title="Shred this office?"
              severity="critical"
              intro={<ShredWarningCopy />}
              confirmLabel="Shred office"
              busy={busy}
              onCancel={() => setOpen(false)}
              onConfirm={() => {
                setBusy(true);
                shredTimerRef.current = setTimeout(() => {
                  setBusy(false);
                  setOpen(false);
                  showNotice("Shredded", "error");
                  shredTimerRef.current = null;
                }, 600);
              }}
            />
          ) : null}
          <ToastContainer />
        </div>
      );
    }
    return <Wrapper />;
  },
};

export const Warn: Story = {
  render: () => {
    function Wrapper() {
      const [open, setOpen] = useState(true);
      return (
        <div style={{ minHeight: 400, padding: 24 }}>
          <button
            type="button"
            className="btn"
            onClick={() => setOpen(true)}
            disabled={open}
          >
            Open wipe modal
          </button>
          {open ? (
            <WipeModal
              title="Reset all sessions?"
              severity="warn"
              intro="This signs every agent out and clears in-memory provider state."
              confirmLabel="Reset sessions"
              busy={false}
              onCancel={() => setOpen(false)}
              onConfirm={() => setOpen(false)}
            />
          ) : null}
        </div>
      );
    }
    return <Wrapper />;
  },
};
