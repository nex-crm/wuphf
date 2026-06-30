// Settings — kept deliberately small. Voice (the call) economics are the
// load-bearing decision here: bring your own OpenAI Realtime key, or let Nex
// host it; with no key the call is optional and chat-authoring is the floor.
// The Voice group is REAL (persists to the broker config); the rest is mock.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { get, post } from "../../api/client";
import { Eyebrow, SurfaceHeader } from "../components/primitives";

interface ConfigStatus {
  openai_key_set?: boolean;
  realtime_model?: string;
}

export function SettingsSurface() {
  const [nexHosted, setNexHosted] = useState(false);
  const [digestOn, setDigestOn] = useState(true);
  const [approvalsOn, setApprovalsOn] = useState(true);

  // The Voice group persists to the broker config so the real call can mint
  // ephemeral Realtime tokens from the key. The key itself is write-only: we
  // never read it back, only whether one is set.
  const qc = useQueryClient();
  const config = useQuery({
    queryKey: ["operator-config"],
    queryFn: () => get<ConfigStatus>("/config"),
  });
  const keySet = Boolean(config.data?.openai_key_set);
  const [keyInput, setKeyInput] = useState("");
  const [modelInput, setModelInput] = useState("");
  const save = useMutation({
    mutationFn: (body: { openai_api_key?: string; realtime_model?: string }) =>
      post("/config", body),
    onSuccess: () => {
      setKeyInput("");
      qc.invalidateQueries({ queryKey: ["operator-config"] });
    },
  });
  const model = modelInput || config.data?.realtime_model || "gpt-realtime";

  return (
    <div className="opr-surface-wide">
      <SurfaceHeader
        eyebrow="Settings"
        title="Settings"
        lede="Voice, notifications, and approvals. Everything else your AI handles."
      />

      <div className="opr-settings">
        <div className="opr-set-group">
          <Eyebrow>Voice</Eyebrow>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">
                OpenAI Realtime key
                {keySet ? (
                  <span className="opr-pill opr-pill-good opr-set-pill">
                    Connected
                  </span>
                ) : null}
              </div>
              <div className="opr-set-help">
                Powers the real screen-share call where you build tools by
                talking. Your key is stored on your broker and never sent to the
                browser. With no key, the call is the guided preview.
              </div>
            </div>
            <input
              className="opr-input"
              type="password"
              aria-label="OpenAI Realtime key"
              placeholder={keySet ? "•••• stored — paste to replace" : "sk-..."}
              value={keyInput}
              onChange={(e) => setKeyInput(e.target.value)}
              disabled={nexHosted}
            />
          </div>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">Realtime model</div>
              <div className="opr-set-help">
                The OpenAI Realtime model the call uses. Leave as the default
                unless your account needs a different one.
              </div>
            </div>
            <input
              className="opr-input"
              aria-label="Realtime model"
              placeholder="gpt-realtime"
              value={model}
              onChange={(e) => setModelInput(e.target.value)}
              disabled={nexHosted}
            />
          </div>
          <div className="opr-set-row" style={{ justifyContent: "flex-end" }}>
            <button
              type="button"
              className="opr-btn opr-btn-primary opr-btn-sm"
              disabled={
                save.isPending || nexHosted || !(keyInput || modelInput)
              }
              onClick={() =>
                save.mutate({
                  ...(keyInput ? { openai_api_key: keyInput } : {}),
                  realtime_model: model,
                })
              }
            >
              {save.isPending ? "Saving…" : "Save voice settings"}
            </button>
          </div>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">Let wuphf host voice for me</div>
              <div className="opr-set-help">
                Metered through wuphf cloud. With no key and this off, the call
                is the guided preview. You can still build everything from chat.
              </div>
            </div>
            <button
              type="button"
              aria-pressed={nexHosted}
              aria-label="Let wuphf host voice for me"
              className={`opr-toggle${nexHosted ? " is-on" : ""}`}
              onClick={() => setNexHosted((v) => !v)}
            />
          </div>
        </div>

        <div className="opr-set-group">
          <Eyebrow>Notifications</Eyebrow>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">Daily digest</div>
              <div className="opr-set-help">
                A morning summary of what your tools did, and anything that
                needs you, in Slack and email.
              </div>
            </div>
            <button
              type="button"
              aria-pressed={digestOn}
              aria-label="Daily digest"
              className={`opr-toggle${digestOn ? " is-on" : ""}`}
              onClick={() => setDigestOn((v) => !v)}
            />
          </div>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">Deliver to</div>
              <div className="opr-set-help">Where digests and alerts go.</div>
            </div>
            <input
              className="opr-input"
              aria-label="Deliver digests and alerts to"
              defaultValue="#revops · maya@company.com"
            />
          </div>
        </div>

        <div className="opr-set-group">
          <Eyebrow>Approvals</Eyebrow>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">Ask before sending externally</div>
              <div className="opr-set-help">
                Your AI checks with you before any tool writes to an outside app
                such as posting to Slack or updating the CRM. Recommended on.
              </div>
            </div>
            <button
              type="button"
              aria-pressed={approvalsOn}
              aria-label="Ask before sending externally"
              className={`opr-toggle${approvalsOn ? " is-on" : ""}`}
              onClick={() => setApprovalsOn((v) => !v)}
            />
          </div>
        </div>

        <div className="opr-set-group">
          <Eyebrow>Danger zone</Eyebrow>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label opr-danger">Delete workspace</div>
              <div className="opr-set-help">
                Removes every tool, its data, and the connected apps. Cannot be
                undone.
              </div>
            </div>
            <button type="button" className="opr-btn opr-btn-sm opr-danger">
              Delete
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
