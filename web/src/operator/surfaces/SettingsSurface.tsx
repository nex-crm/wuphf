// Settings — kept deliberately small. Voice (the call) economics are the
// load-bearing decision here: bring your own OpenAI Realtime key, or let Nex
// host it; with no key the call is optional and chat-authoring is the floor.
// Mock data; toggles flip local state only.

import { useState } from "react";

import { Eyebrow, SurfaceHeader } from "../components/primitives";

export function SettingsSurface() {
  const [nexHosted, setNexHosted] = useState(false);
  const [digestOn, setDigestOn] = useState(true);
  const [approvalsOn, setApprovalsOn] = useState(true);

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
              <div className="opr-set-label">OpenAI Realtime key</div>
              <div className="opr-set-help">
                Powers the screen-share call where you build tools by talking.
                Bring your own key, or let wuphf host it.
              </div>
            </div>
            <input
              className="opr-input"
              type="password"
              aria-label="OpenAI Realtime key"
              placeholder="sk-..."
              defaultValue=""
              disabled={nexHosted}
            />
          </div>
          <div className="opr-set-row">
            <div>
              <div className="opr-set-label">Let wuphf host voice for me</div>
              <div className="opr-set-help">
                Metered through wuphf cloud. With no key and this off, the call is
                optional. You can still build everything from chat.
              </div>
            </div>
            <button
              type="button"
              aria-pressed={nexHosted}
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
