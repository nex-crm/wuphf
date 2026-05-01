import { useState } from "react";

import type { API_KEY_FIELDS } from "./constants";

// ApiKeyRow renders one provider's CLI-login-vs-API-key choice. Default
// path is CLI login (most users have one of claude/codex/gcloud logged
// in already from the runtime grid above); clicking "Use API key"
// reveals the password input so users can paste without exposing the
// key the whole time. If the user has any value typed in, the input
// stays open even after toggling away — we don't drop their key.

interface ApiKeyRowProps {
  field: (typeof API_KEY_FIELDS)[number];
  value: string;
  onChange: (v: string) => void;
}

export function ApiKeyRow({ field, value, onChange }: ApiKeyRowProps) {
  const [showInput, setShowInput] = useState<boolean>(value.length > 0);
  const useApiKey = showInput || value.length > 0;
  return (
    <div className="key-row" data-testid={`api-key-row-${field.key}`}>
      <div className="key-label-wrap">
        <span className="key-label">{field.label}</span>
        <span className="key-hint">{field.hint}</span>
      </div>
      <div
        className="key-input-wrap"
        style={{ display: "flex", flexDirection: "column", gap: 6 }}
      >
        <div style={{ display: "flex", gap: 8 }}>
          <button
            type="button"
            className={`runtime-tile ${!useApiKey ? "selected" : ""}`}
            onClick={() => {
              setShowInput(false);
              if (value) onChange("");
            }}
            aria-pressed={!useApiKey}
            data-testid={`api-key-cli-${field.key}`}
            style={{ padding: "6px 10px", fontSize: 12, minWidth: 0 }}
          >
            Use CLI login
          </button>
          <button
            type="button"
            className={`runtime-tile ${useApiKey ? "selected" : ""}`}
            onClick={() => setShowInput(true)}
            aria-pressed={useApiKey}
            data-testid={`api-key-paste-${field.key}`}
            style={{ padding: "6px 10px", fontSize: 12, minWidth: 0 }}
          >
            Use API key
          </button>
        </div>
        {!useApiKey && (
          <p
            style={{
              fontSize: 11,
              color: "var(--text-tertiary)",
              margin: 0,
              fontFamily: "var(--font-mono)",
            }}
          >
            Run <code>{field.cliLoginCmd}</code> in a terminal — agents pick up
            the session automatically.
          </p>
        )}
        {useApiKey ? (
          <input
            className="input"
            type="password"
            placeholder={field.key}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            autoComplete="off"
            data-testid={`api-key-input-${field.key}`}
          />
        ) : null}
      </div>
    </div>
  );
}
