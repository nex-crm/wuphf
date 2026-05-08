import { useState } from "react";

import type { API_KEY_FIELDS } from "./constants";

interface ApiKeyRowProps {
  field: (typeof API_KEY_FIELDS)[number];
  value: string;
  onChange: (v: string) => void;
}

export function ApiKeyRow({ field, value, onChange }: ApiKeyRowProps) {
  const [showInput, setShowInput] = useState<boolean>(value.length > 0);
  const useApiKey = showInput || value.length > 0;
  const panelId = `api-key-panel-${field.key}`;
  return (
    <div className="key-row" data-testid={`api-key-row-${field.key}`}>
      <div className="key-label-wrap">
        <label
          className="key-label"
          htmlFor={`api-key-input-${field.key}`}
          id={`api-key-label-${field.key}`}
        >
          {field.label}
        </label>
        <span className="key-hint" id={`api-key-hint-${field.key}`}>
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
            className={`key-tab ${!useApiKey ? "active" : ""}`}
            onClick={() => {
              setShowInput(false);
              if (value) onChange("");
            }}
            aria-selected={!useApiKey}
            aria-controls={panelId}
            tabIndex={!useApiKey ? 0 : -1}
            data-testid={`api-key-cli-${field.key}`}
          >
            CLI login
          </button>
          <button
            type="button"
            role="tab"
            className={`key-tab ${useApiKey ? "active" : ""}`}
            onClick={() => setShowInput(true)}
            aria-selected={useApiKey}
            aria-controls={panelId}
            tabIndex={useApiKey ? 0 : -1}
            data-testid={`api-key-paste-${field.key}`}
          >
            API key
          </button>
        </div>
        <div
          id={panelId}
          role="tabpanel"
          aria-labelledby={
            useApiKey
              ? `api-key-paste-${field.key}`
              : `api-key-cli-${field.key}`
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
              id={`api-key-input-${field.key}`}
              className="input"
              type="password"
              style={{ height: 36 }}
              placeholder={field.key}
              value={value}
              onChange={(e) => onChange(e.target.value)}
              autoComplete="off"
              aria-labelledby={`api-key-label-${field.key}`}
              aria-describedby={`api-key-hint-${field.key}`}
              data-testid={`api-key-input-${field.key}`}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}
