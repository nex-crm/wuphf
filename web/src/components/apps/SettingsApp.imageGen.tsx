import { type CSSProperties, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getImageProviders,
  type ImageProviderStatus,
  setImageProviderConfig,
} from "../../api/client";
import { showNotice } from "../ui/Toast";

const labelStyle: CSSProperties = {
  display: "block",
  fontSize: 11,
  fontWeight: 600,
  textTransform: "uppercase",
  letterSpacing: "0.05em",
  color: "var(--text-tertiary)",
  marginBottom: 6,
};

const inputStyle: CSSProperties = {
  width: "100%",
  padding: "8px 10px",
  fontSize: 13,
  background: "var(--bg-warm)",
  color: "var(--text)",
  border: "1px solid var(--border)",
  borderRadius: 6,
  fontFamily: "var(--font-mono)",
};

function StatusDot({ s }: { s: ImageProviderStatus }) {
  let color = "var(--text-tertiary)";
  let title = "Unknown";
  if (s.implementation_ok && s.configured) {
    color = "#16a34a";
    title = "Configured + ready";
  } else if (s.implementation_ok && s.needs_api_key && !s.api_key_set) {
    color = "#d97706";
    title = "API key missing";
  } else if (!s.implementation_ok) {
    color = "#6b7280";
    title = "Stub — backend pending";
  } else {
    color = "#dc2626";
    title = "Misconfigured";
  }
  return (
    <span
      title={title}
      style={{
        display: "inline-block",
        width: 10,
        height: 10,
        borderRadius: "50%",
        background: color,
        marginRight: 8,
        flexShrink: 0,
      }}
    />
  );
}

function ProviderCard({ s }: { s: ImageProviderStatus }) {
  const qc = useQueryClient();
  const [apiKey, setApiKey] = useState("");
  const [baseURL, setBaseURL] = useState(s.base_url ?? "");
  const [model, setModel] = useState(s.default_model ?? "");
  const [showKey, setShowKey] = useState(false);

  const mutation = useMutation({
    mutationFn: () =>
      setImageProviderConfig({
        kind: s.kind,
        api_key: apiKey ? apiKey : undefined,
        base_url: baseURL ? baseURL : undefined,
        model: model ? model : undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["image-providers"] });
      showNotice(`${s.label} updated`, "success");
      setApiKey("");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Update failed";
      showNotice(msg, "error");
    },
  });

  return (
    <div
      style={{
        background: "var(--bg-card)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: 16,
        marginBottom: 12,
      }}
    >
      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <StatusDot s={s} />
        <h3 style={{ fontSize: 15, fontWeight: 600 }}>{s.label}</h3>
        <span
          style={{
            fontSize: 11,
            color: "var(--text-tertiary)",
            marginLeft: "auto",
          }}
        >
          {s.kind}
          {s.supports_video && " · video"}
          {s.implementation_ok ? "" : " · stub"}
        </span>
      </div>
      <p
        style={{
          fontSize: 12,
          color: "var(--text-secondary)",
          margin: "8px 0 12px",
          lineHeight: 1.45,
        }}
      >
        {s.blurb}
      </p>
      {s.setup_hint && (
        <p
          style={{
            fontSize: 11,
            padding: "6px 10px",
            background: "var(--accent-bg)",
            color: "var(--accent)",
            borderRadius: 4,
            margin: "0 0 12px",
          }}
        >
          {s.setup_hint}
        </p>
      )}

      {s.needs_api_key && (
        <div style={{ marginBottom: 10 }}>
          <label style={labelStyle} htmlFor={`${s.kind}-api-key`}>
            API key {s.api_key_set ? "(set)" : "(unset)"}
          </label>
          <div style={{ display: "flex", gap: 8 }}>
            <input
              id={`${s.kind}-api-key`}
              type={showKey ? "text" : "password"}
              value={apiKey}
              placeholder={s.api_key_set ? "•••••• (replace)" : "paste here"}
              onChange={(e) => setApiKey(e.target.value)}
              style={inputStyle}
            />
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => setShowKey((v) => !v)}
              style={{ flexShrink: 0 }}
            >
              {showKey ? "hide" : "show"}
            </button>
          </div>
        </div>
      )}

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 10,
          marginBottom: 10,
        }}
      >
        <div>
          <label style={labelStyle} htmlFor={`${s.kind}-base-url`}>
            Base URL (optional)
          </label>
          <input
            id={`${s.kind}-base-url`}
            type="text"
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
            placeholder={s.base_url || "default"}
            style={inputStyle}
          />
        </div>
        <div>
          <label style={labelStyle} htmlFor={`${s.kind}-model`}>
            Default model
          </label>
          <input
            id={`${s.kind}-model`}
            type="text"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder={s.default_model || "default"}
            style={inputStyle}
          />
        </div>
      </div>

      <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={mutation.isPending}
          onClick={() => mutation.mutate()}
        >
          {mutation.isPending ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

export function ImageGenSection() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["image-providers"],
    queryFn: getImageProviders,
    refetchInterval: 60_000,
  });

  if (isLoading) {
    return (
      <div style={{ padding: 24, color: "var(--text-tertiary)" }}>Loading…</div>
    );
  }
  if (error) {
    return (
      <div style={{ padding: 24, color: "var(--text-tertiary)" }}>
        Could not load image providers:{" "}
        {error instanceof Error ? error.message : String(error)}
      </div>
    );
  }
  const providers = data?.providers ?? [];

  return (
    <div style={{ padding: "20px 24px" }}>
      <header style={{ marginBottom: 16 }}>
        <h2 style={{ fontSize: 18, fontWeight: 600 }}>Image generation</h2>
        <p
          style={{
            fontSize: 13,
            color: "var(--text-secondary)",
            marginTop: 6,
            lineHeight: 1.5,
          }}
        >
          Backends Artist can call via the <code>image_generate</code> tool.
          Paste an API key + (optional) base URL + default model. Status dot:
          green = ready, amber = needs key, grey = stub (backend not yet wired).
        </p>
      </header>
      {providers.map((p) => (
        <ProviderCard key={p.kind} s={p} />
      ))}
    </div>
  );
}
