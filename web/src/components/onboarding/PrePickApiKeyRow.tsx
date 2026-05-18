import { useState } from "react";

import type { ApiKeyFieldDef } from "./runtimeConstants";

interface PrePickApiKeyRowProps {
  field: ApiKeyFieldDef;
  value: string;
  onChange: (v: string) => void;
}

/**
 * Single API-key row with a two-mode toggle: "CLI login" shows the provider's
 * login command, "API key" reveals a masked password input.
 *
 * Ported from the deleted wizard/ApiKeyRow.tsx — same UX, same CSS classes,
 * now living at the onboarding/ level so PrePickScreen can use it directly.
 */
export function PrePickApiKeyRow({
  field,
  value,
  onChange,
}: PrePickApiKeyRowProps) {
  const [showInput, setShowInput] = useState<boolean>(value.length > 0);
  const useApiKey = showInput || value.length > 0;
  const panelId = `pre-pick-api-panel-${field.key}`;

  return (
    <div className="key-row" data-testid={`pre-pick-api-row-${field.key}`}>
      <div className="key-label-wrap">
        <label
          className="key-label"
          htmlFor={`pre-pick-api-input-${field.key}`}
          id={`pre-pick-api-label-${field.key}`}
        >
          {field.label}
        </label>
        <span className="key-hint" id={`pre-pick-api-hint-${field.key}`}>
          {field.hint}
        </span>
      </div>
      <div
        className="key-input-wrap"
        style={{ display: "flex", flexDirection: "column", gap: 8 }}
      >
        <div
          className="key-tabs"
          role="tablist"
          aria-label={`${field.label} auth method`}
        >
          <button
            type="button"
            role="tab"
            className={`key-tab${!useApiKey ? " active" : ""}`}
            onClick={() => {
              setShowInput(false);
              if (value) onChange("");
            }}
            aria-selected={!useApiKey}
            aria-controls={panelId}
            tabIndex={!useApiKey ? 0 : -1}
            data-testid={`pre-pick-api-cli-${field.key}`}
          >
            CLI login
          </button>
          <button
            type="button"
            role="tab"
            className={`key-tab${useApiKey ? " active" : ""}`}
            onClick={() => setShowInput(true)}
            aria-selected={useApiKey}
            aria-controls={panelId}
            tabIndex={useApiKey ? 0 : -1}
            data-testid={`pre-pick-api-paste-${field.key}`}
          >
            API key
          </button>
        </div>
        <div
          id={panelId}
          role="tabpanel"
          aria-labelledby={
            useApiKey
              ? `pre-pick-api-paste-${field.key}`
              : `pre-pick-api-cli-${field.key}`
          }
        >
          {!useApiKey && (
            <p
              style={{
                fontSize: 12,
                lineHeight: 1.45,
                minHeight: 36,
                color: "var(--text-secondary)",
                margin: 0,
              }}
            >
              Run <code>{field.cliLoginCmd}</code> in a terminal — agents pick
              up the session automatically.
            </p>
          )}
          {useApiKey ? (
            <input
              id={`pre-pick-api-input-${field.key}`}
              className="input"
              type="password"
              style={{ height: 36 }}
              placeholder={field.key}
              value={value}
              onChange={(e) => onChange(e.target.value)}
              autoComplete="off"
              aria-labelledby={`pre-pick-api-label-${field.key}`}
              aria-describedby={`pre-pick-api-hint-${field.key}`}
              data-testid={`pre-pick-api-input-${field.key}`}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}
