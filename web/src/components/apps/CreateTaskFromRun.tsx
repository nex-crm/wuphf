import { type CSSProperties, useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { type RunRecord, runToTask } from "../../api/workflows";
import { router } from "../../lib/router";

/**
 * CreateTaskFromRun — the "kick off a task from this run" control shown on a
 * workflow run (both the just-ran details and each run-history row). It folds
 * the run's outcome into a new task's context and lets the operator add a start
 * prompt; the created task is owned by the CEO, which triages + kicks it off.
 */
export default function CreateTaskFromRun({
  specId,
  rec,
}: {
  specId: string;
  rec: RunRecord;
}) {
  const [open, setOpen] = useState(false);
  const [prompt, setPrompt] = useState("");
  const create = useMutation({
    mutationFn: () => runToTask(specId, prompt, rec),
    onSuccess: (res) => {
      // Jump straight to the new task — its channel now opens with the run
      // context + this prompt as the kickoff message.
      void router.navigate({
        to: "/tasks/$taskId",
        params: { taskId: res.task_id },
      });
    },
  });

  if (create.isSuccess) {
    return (
      <div style={{ fontSize: 12.5, color: "var(--green)" }}>
        ✓ Task created — opening…
      </div>
    );
  }

  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} style={ghostBtn}>
        ⊕ Create task from this run
      </button>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div style={{ fontSize: 12.5, color: "var(--text-secondary)" }}>
        Kick off a task with this run as context. Add a start prompt:
      </div>
      <textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        placeholder="e.g. Draft replies to the emails that need a response and put them in my drafts."
        rows={3}
        style={{
          width: "100%",
          fontSize: 13,
          fontFamily: "inherit",
          color: "var(--text)",
          background: "var(--bg-card)",
          border: "1px solid var(--border)",
          borderRadius: 8,
          padding: "8px 10px",
          resize: "vertical",
        }}
      />
      {create.error && (
        <div style={{ fontSize: 12, color: "var(--red)" }}>
          {create.error instanceof Error
            ? create.error.message
            : "Couldn't create the task"}
        </div>
      )}
      <div style={{ display: "flex", gap: 8 }}>
        <button
          type="button"
          onClick={() => create.mutate()}
          disabled={create.isPending}
          style={primaryBtn(create.isPending)}
        >
          {create.isPending ? "Creating…" : "Create task"}
        </button>
        <button type="button" onClick={() => setOpen(false)} style={ghostBtn}>
          Cancel
        </button>
      </div>
    </div>
  );
}

function primaryBtn(busy: boolean): CSSProperties {
  return {
    background: "var(--accent)",
    color: "#fff",
    border: "none",
    borderRadius: 8,
    padding: "8px 14px",
    fontSize: 13,
    fontWeight: 600,
    cursor: busy ? "default" : "pointer",
    opacity: busy ? 0.7 : 1,
  };
}

const ghostBtn: CSSProperties = {
  background: "transparent",
  color: "var(--text-secondary)",
  border: "1px solid var(--border)",
  borderRadius: 8,
  padding: "6px 12px",
  fontSize: 12.5,
  fontWeight: 550,
  cursor: "pointer",
};
