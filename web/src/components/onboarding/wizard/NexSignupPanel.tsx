import type { NexSignupStatus } from "./types";

// NexSignupPanel is the optional "don't have a Nex account yet?"
// affordance rendered inside IdentityStep. It's compact by default
// (one-line link) so users with a key already aren't distracted. The
// primary path calls /nex/register on the broker, which shells out to
// `nex-cli setup <email>`. If nex-cli isn't installed, the broker
// returns 502 with ErrNotInstalled and we flip to the external-link
// fallback (open nex.ai/register + paste key on Setup step). Matches
// the TUI's InitNexRegister phase in init_flow.go.

interface NexSignupPanelProps {
  email: string;
  status: NexSignupStatus;
  error: string;
  onChangeEmail: (v: string) => void;
  onSubmit: () => void;
}

export function NexSignupPanel({
  email,
  status,
  error,
  onChangeEmail,
  onSubmit,
}: NexSignupPanelProps) {
  return (
    <div
      className="wizard-panel wiz-nex-signup"
      style={{ display: "flex", flexDirection: "column", gap: 24 }}
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        {status !== "fallback" && status !== "ok" && (
          <p
            style={{
              fontSize: 14,
              fontWeight: 600,
              color: "var(--text)",
              margin: 0,
            }}
          >
            Register an email to get a free Nex API key
          </p>
        )}
        <p
          style={{
            fontSize: 13,
            color: "var(--text-secondary)",
            margin: 0,
          }}
        >
          {status === "fallback" ? (
            "nex-cli is not installed on this machine. Register in your browser, then paste the key from Settings → Integrations after launch."
          ) : (
            <>
              Powers shared memory, entity briefs, and integrations.
              <br />
              You can also paste an existing key from Settings → Integrations
              once the office is open.
            </>
          )}
        </p>
      </div>

      {status === "fallback" ? (
        <a
          className="btn btn-secondary"
          href="https://nex.ai/register"
          target="_blank"
          rel="noopener noreferrer"
        >
          Open nex.ai/register
        </a>
      ) : status === "ok" ? (
        <p className="wiz-nex-ok" role="status">
          Check your inbox at {email} for the Nex API key, then paste it from
          Settings → Integrations once the office is open.
        </p>
      ) : (
        <div className="form-group" style={{ margin: 0 }}>
          <label className="label" htmlFor="wiz-nex-email">
            Email
          </label>
          <input
            className="input"
            id="wiz-nex-email"
            type="email"
            placeholder="you@example.com"
            value={email}
            onChange={(e) => onChangeEmail(e.target.value)}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                status !== "submitting" &&
                email.trim().length > 0
              ) {
                e.preventDefault();
                e.stopPropagation();
                onSubmit();
              }
            }}
            disabled={status === "submitting"}
          />
          {error ? (
            <p
              style={{ color: "var(--red)", fontSize: 12, marginTop: 6 }}
              role="alert"
            >
              {error}
            </p>
          ) : null}
        </div>
      )}
    </div>
  );
}
