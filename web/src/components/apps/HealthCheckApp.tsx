import { useQuery } from "@tanstack/react-query";

import { getHealth } from "../../api/platform";

export function HealthCheckApp() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["health"],
    queryFn: () => getHealth(),
    refetchInterval: 10_000,
  });

  if (isLoading) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Checking health...
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Could not reach health endpoint.
      </div>
    );
  }

  const status = data?.status ?? "unknown";
  const isHealthy = status === "ok" || status === "healthy";
  const providerLabel = [data?.provider, data?.provider_model]
    .filter(Boolean)
    .join(" / ");
  const sessionLabel =
    data?.session_mode === "one_on_one" && data.one_on_one_agent
      ? `${data.session_mode} / ${data.one_on_one_agent}`
      : data?.session_mode;
  const memoryLabel = data?.memory_backend_active || data?.memory_backend;
  const runtimeItems = [
    {
      label: "Session",
      value: sessionLabel || "unknown",
      active: Boolean(data?.session_mode),
    },
    {
      label: "Provider",
      value: providerLabel || "unknown",
      active: Boolean(data?.provider),
    },
    {
      label: "Memory",
      value: memoryLabel || "none",
      active: Boolean(data?.memory_backend_ready),
    },
    {
      label: "Nex",
      value: data?.nex_connected ? "connected" : "disconnected",
      active: Boolean(data?.nex_connected),
    },
    {
      label: "Build",
      value: data?.build?.version ?? "unknown",
      active: Boolean(data?.build?.version),
    },
  ];

  return (
    <>
      <div
        style={{
          padding: "0 0 12px",
          borderBottom: "1px solid var(--border)",
          marginBottom: 12,
        }}
      >
        <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 4 }}>
          Health Check
        </h3>
      </div>

      {/* Overall status */}
      <div
        className="app-card"
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          marginBottom: 12,
        }}
      >
        <span
          className={`status-dot ${isHealthy ? "active" : ""}`}
          style={{ width: 10, height: 10 }}
        />
        <div>
          <div style={{ fontWeight: 600, fontSize: 14 }}>Broker Status</div>
          <div className="app-card-meta">
            <span
              className={isHealthy ? "badge badge-green" : "badge badge-yellow"}
            >
              {status.toUpperCase()}
            </span>
          </div>
        </div>
      </div>

      <div
        style={{
          fontSize: 11,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "var(--text-tertiary)",
          padding: "8px 0 6px",
        }}
      >
        Runtime
      </div>
      {runtimeItems.map((item) => (
        <div
          key={item.label}
          className="app-card"
          style={{
            marginBottom: 6,
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <span className={`status-dot ${item.active ? "active" : ""}`} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 500, fontSize: 13 }}>{item.label}</div>
            <div
              className="app-card-meta"
              style={{
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {item.value}
            </div>
          </div>
        </div>
      ))}

      {data?.focus_mode ? (
        <div
          className="app-card"
          style={{
            marginTop: 12,
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <span className="status-dot active" />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 500, fontSize: 13 }}>Focus Mode</div>
            <div className="app-card-meta">enabled</div>
          </div>
        </div>
      ) : null}
    </>
  );
}
