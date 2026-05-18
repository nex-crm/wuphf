/**
 * IssueNewForm — manual issue draft creation surface (/issues/new).
 *
 * Lets a human file an issue without going through the CEO chat. The
 * resulting task starts in the broker's intake bucket so the owner agent
 * can pick it up; on success we hand the user straight to the new issue's
 * detail surface so they can iterate from there.
 */

import { type FormEvent, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { createTasks } from "../../api/tasks";
import { router } from "../../lib/router";

const DEFAULT_CHANNEL = "general";

export function IssueNewForm() {
  const [title, setTitle] = useState("");
  const [details, setDetails] = useState("");
  const [channel, setChannel] = useState(DEFAULT_CHANNEL);
  const [assignee, setAssignee] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const titleRef = useRef<HTMLInputElement | null>(null);

  const queryClient = useQueryClient();

  useEffect(() => {
    titleRef.current?.focus();
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      setError("Title is required.");
      return;
    }

    setError(null);
    setSubmitting(true);
    try {
      const response = await createTasks(
        [
          {
            title: trimmedTitle,
            assignee: assignee.trim() || "human",
            details: details.trim() || undefined,
          },
        ],
        { channel: channel.trim() || DEFAULT_CHANNEL, createdBy: "human" },
      );
      const created = response.tasks?.[0];
      void queryClient.invalidateQueries({ queryKey: ["issues"] });
      void queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
      if (created?.id) {
        void router.navigate({
          to: "/issues/$issueId",
          params: { issueId: created.id },
          replace: true,
        });
      } else {
        void router.navigate({ to: "/issues", replace: true });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not file issue.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="app-panel active"
      data-testid="issue-new"
      style={{ overflowY: "auto" }}
    >
      <form
        className="issue-new-form"
        onSubmit={handleSubmit}
        noValidate={true}
      >
        <h2 className="issue-new-form-heading">File a new issue</h2>
        <p className="issue-new-form-hint">
          The owner agent picks this up from intake. Drop the details now or let
          them ask follow-up questions in the channel.
        </p>

        <div className="issue-new-form-field">
          <label htmlFor="issue-new-title">Title</label>
          <input
            id="issue-new-title"
            ref={titleRef}
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What needs to happen?"
            required={true}
            data-testid="issue-new-title"
          />
        </div>

        <div className="issue-new-form-field">
          <label htmlFor="issue-new-details">Details</label>
          <textarea
            id="issue-new-details"
            value={details}
            onChange={(e) => setDetails(e.target.value)}
            placeholder="Acceptance criteria, links, constraints…"
            data-testid="issue-new-details"
          />
        </div>

        <div
          style={{
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: 12,
          }}
        >
          <div className="issue-new-form-field">
            <label htmlFor="issue-new-channel">Channel</label>
            <input
              id="issue-new-channel"
              type="text"
              value={channel}
              onChange={(e) => setChannel(e.target.value)}
              placeholder="general"
              data-testid="issue-new-channel"
            />
          </div>
          <div className="issue-new-form-field">
            <label htmlFor="issue-new-assignee">Assignee (optional)</label>
            <input
              id="issue-new-assignee"
              type="text"
              value={assignee}
              onChange={(e) => setAssignee(e.target.value)}
              placeholder="agent slug — leave blank to self-assign"
              data-testid="issue-new-assignee"
            />
          </div>
        </div>

        {error ? (
          <p
            className="issue-new-form-error"
            role="alert"
            data-testid="issue-new-error"
          >
            {error}
          </p>
        ) : null}

        <div className="issue-new-form-actions">
          <button
            type="button"
            className="issues-list-retry-btn"
            onClick={() => void router.navigate({ to: "/issues" })}
            disabled={submitting}
          >
            Cancel
          </button>
          <button
            type="submit"
            className="issues-new-btn"
            disabled={submitting}
            data-testid="issue-new-submit"
          >
            {submitting ? "Filing…" : "File issue"}
          </button>
        </div>
      </form>
    </div>
  );
}
