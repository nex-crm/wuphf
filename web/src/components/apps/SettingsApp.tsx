import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Refresh, WarningTriangle } from "iconoir-react";

import {
  type ConfigSnapshot,
  type ConfigUpdate,
  getConfig,
  getLocalProvidersStatus,
  type LocalProviderStatus,
  resetWorkspace,
  shredWorkspace,
  updateConfig,
  type WorkspaceWipeResult,
} from "../../api/client";
import { useAppStore } from "../../stores/app";
import {
  ShredCardSubtitle,
  ShredDeletionsList,
  ShredPreservationList,
  ShredWarningCopy,
} from "../ui/ShredWarning";
import { showNotice } from "../ui/Toast";
import { WipeModal } from "../ui/WipeModal";
import { ImageGenSection } from "./SettingsApp.imageGen";
import { Field, KeyField, SaveButton } from "./settings/components";
import { SECTION_GROUPS } from "./settings/constants";
import { styles } from "./settings/styles";
import type { SectionId, SectionProps } from "./settings/types";

// ─── Section components ─────────────────────────────────────────────────

// useShredAction wraps `shredWorkspace()` with the cleanup both call sites
// (GeneralSection's inline button and DangerZoneSection's full card) need on
// success: clear the query cache, route the user back to a sensible default
// channel, and reset onboarding state so the wizard reopens. The broker's
// `AfterShred` hook (`internal/team/broker.go`) calls `requestShutdown()`, so
// the page typically re-mounts shortly after — but until it does, the user
// shouldn't be left on a Settings tab whose `cfg` query just got invalidated.
function useShredAction() {
  const queryClient = useQueryClient();
  const resetForOnboarding = useAppStore((s) => s.resetForOnboarding);
  return async (): Promise<boolean> => {
    try {
      const result: WorkspaceWipeResult = await shredWorkspace();
      if (!result.ok) {
        showNotice(result.error || "Shred failed", "error");
        return false;
      }
      queryClient.clear();
      window.history.replaceState(null, "", "#/channels/general");
      resetForOnboarding();
      showNotice("Workspace shredded. Onboarding reopened.", "success");
      return true;
    } catch (err) {
      showNotice(err instanceof Error ? err.message : "Shred failed", "error");
      return false;
    }
  };
}

function GeneralSection({ cfg, save }: SectionProps) {
  const [provider, setProvider] = useState(cfg.llm_provider ?? "ollama");
  const [memory, setMemory] = useState(cfg.memory_backend ?? "nex");
  const [teamLead, setTeamLead] = useState(cfg.team_lead_slug ?? "");
  const [maxConcurrent, setMaxConcurrent] = useState(
    cfg.max_concurrent_agents ? String(cfg.max_concurrent_agents) : "",
  );
  const [format, setFormat] = useState(cfg.default_format ?? "text");
  const [timeout, setTimeoutMs] = useState(
    cfg.default_timeout ? String(cfg.default_timeout) : "",
  );
  const [blueprint, setBlueprint] = useState(cfg.blueprint ?? "");
  const [email, setEmail] = useState(cfg.email ?? "");
  const [devUrl, setDevUrl] = useState(cfg.dev_url ?? "");

  const onSave = async () => {
    const patch: ConfigUpdate = {
      llm_provider: provider as ConfigUpdate["llm_provider"],
      memory_backend: memory as ConfigUpdate["memory_backend"],
      default_format: format,
      blueprint,
      email,
      dev_url: devUrl,
      team_lead_slug: teamLead,
    };
    if (maxConcurrent)
      patch.max_concurrent_agents = parseInt(maxConcurrent, 10);
    if (timeout) patch.default_timeout = parseInt(timeout, 10);
    await save(patch);
  };

  return (
    <div>
      <h2 style={styles.sectionTitle}>General</h2>
      <p style={styles.sectionDesc}>
        Core runtime settings. These map to CLI flags and config file entries.
      </p>

      <div style={styles.groupTitle}>Runtime</div>
      <Field label="LLM Provider" hint="--provider">
        <select
          style={styles.input}
          value={provider}
          onChange={(e) => setProvider(e.target.value as typeof provider)}
        >
          <optgroup label="Cloud">
            <option value="claude-code">Claude Code</option>
            <option value="codex">Codex</option>
            <option value="opencode">Opencode</option>
          </optgroup>
          <optgroup label="Local">
            <option value="mlx-lm">MLX-LM (Apple Silicon)</option>
            <option value="ollama">Ollama</option>
            <option value="exo">Exo</option>
          </optgroup>
        </select>
      </Field>
      <Field label="Memory Backend" hint="--memory-backend">
        <select
          style={styles.input}
          value={memory}
          onChange={(e) => setMemory(e.target.value as typeof memory)}
        >
          <option value="nex">Nex</option>
          <option value="gbrain">GBrain</option>
          <option value="none">None (local only)</option>
        </select>
      </Field>

      <div style={{ ...styles.groupTitle, marginTop: 24 }}>Agents</div>
      <Field label="Team Lead" hint="Default agent that leads operations">
        <input
          style={styles.input}
          placeholder="e.g. ceo"
          value={teamLead}
          onChange={(e) => setTeamLead(e.target.value)}
        />
      </Field>
      <Field label="Max Concurrent" hint="Parallel agent limit">
        <input
          style={styles.input}
          type="number"
          min={1}
          placeholder="Unlimited"
          value={maxConcurrent}
          onChange={(e) => setMaxConcurrent(e.target.value)}
        />
      </Field>

      <div style={{ ...styles.groupTitle, marginTop: 24 }}>Defaults</div>
      <Field label="Output Format" hint="--format">
        <select
          style={styles.input}
          value={format}
          onChange={(e) => setFormat(e.target.value)}
        >
          <option value="text">Text</option>
          <option value="json">JSON</option>
        </select>
      </Field>
      <Field label="Timeout (ms)" hint="Default command timeout">
        <input
          style={styles.input}
          type="number"
          min={1000}
          placeholder="120000"
          value={timeout}
          onChange={(e) => setTimeoutMs(e.target.value)}
        />
      </Field>

      <div style={{ ...styles.groupTitle, marginTop: 24 }}>Identity</div>
      <Field label="Blueprint" hint="--blueprint">
        <input
          style={styles.input}
          placeholder="Operation blueprint ID"
          value={blueprint}
          onChange={(e) => setBlueprint(e.target.value)}
        />
      </Field>
      <Field label="Email" hint="Identity scope for integrations">
        <input
          style={styles.input}
          type="email"
          placeholder="you@company.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </Field>
      <Field label="Dev URL" hint="API base URL override">
        <input
          style={styles.input}
          placeholder="https://app.nex.ai"
          value={devUrl}
          onChange={(e) => setDevUrl(e.target.value)}
        />
      </Field>

      <div style={{ marginTop: 24 }}>
        <SaveButton label="Save general settings" onSave={onSave} />
      </div>

      {cfg.config_path ? (
        <div style={{ marginTop: 24 }}>
          <div style={styles.groupTitle}>Config file</div>
          <div style={styles.filePath}>{cfg.config_path}</div>
        </div>
      ) : null}
    </div>
  );
}

// ─── Local LLMs section ─────────────────────────────────────────────────

interface LocalProviderMeta {
  kind: string;
  label: string;
  blurb: string;
}

const LOCAL_PROVIDERS: LocalProviderMeta[] = [
  {
    kind: "mlx-lm",
    label: "MLX-LM",
    blurb:
      "Apple's MLX-backed inference server. Apple Silicon only. Best fit for native macOS performance.",
  },
  {
    kind: "ollama",
    label: "Ollama",
    blurb:
      "Cross-platform local model runner with the largest model catalog. Works on macOS and Linux.",
  },
  {
    kind: "exo",
    label: "Exo",
    blurb:
      "Distributes inference across multiple devices. Useful when you want to pool a Mac Studio + a laptop.",
  },
];

function detectHostPlatform(): "macos" | "linux" | "windows" | "other" {
  if (typeof navigator === "undefined") return "other";
  const p = navigator.platform.toLowerCase();
  const ua = navigator.userAgent.toLowerCase();
  if (p.includes("mac") || ua.includes("mac os")) return "macos";
  if (p.includes("linux") || ua.includes("linux")) return "linux";
  if (p.includes("win") || ua.includes("windows")) return "windows";
  return "other";
}

function StatusDot({ status }: { status: LocalProviderStatus | undefined }) {
  let color = "var(--text-tertiary)";
  let title = "Status unknown";
  if (!status) {
    /* default */
  } else if (status.binary_installed && status.reachable) {
    color = "#16a34a"; // green
    title = `Running${status.loaded_model ? ` · ${status.loaded_model}` : ""}`;
  } else if (status.binary_installed) {
    color = "#d97706"; // yellow/amber
    title = "Installed but server not reachable — start it from a terminal";
  } else {
    color = "#dc2626"; // red
    title = "Not installed";
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

function CommandRow({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      showNotice("Copy failed — select the text and copy manually.", "error");
    }
  };
  return (
    <div
      style={{
        display: "flex",
        gap: 8,
        alignItems: "center",
        padding: "6px 8px",
        background: "var(--bg-card-soft, var(--bg-card))",
        border: "1px solid var(--border-light)",
        borderRadius: 4,
        marginTop: 6,
      }}
    >
      <code
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          flex: 1,
          overflowX: "auto",
          whiteSpace: "nowrap",
        }}
      >
        {command}
      </code>
      <button
        type="button"
        className="btn btn-secondary btn-sm"
        onClick={onCopy}
        style={{ flexShrink: 0 }}
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}

interface LocalProviderCardProps {
  meta: LocalProviderMeta;
  status: LocalProviderStatus | undefined;
  cfg: ConfigSnapshot;
  save: (patch: ConfigUpdate) => Promise<void>;
  hostPlatform: ReturnType<typeof detectHostPlatform>;
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
function LocalProviderCard({
  meta,
  status,
  cfg,
  save,
  hostPlatform,
}: LocalProviderCardProps) {
  // Inputs default to the user's saved override (if any) and otherwise
  // empty so placeholders show the resolved value. We must NOT seed the
  // input with the resolved compile-time default — Save would then
  // persist that default as a permanent override on /config, locking
  // future users out of upstream default changes.
  const initial = cfg.provider_endpoints?.[meta.kind];
  const [baseURL, setBaseURL] = useState(initial?.base_url ?? "");
  const [model, setModel] = useState(initial?.model ?? "");

  // Save is gated on dirty so an empty form (or a form matching the
  // saved override) doesn't write anything. The empty-empty case is
  // also the "clear back to defaults" gesture handled server-side
  // (broker.go:6244) — but only when the user previously had an
  // override; submitting empty-empty against no-override is a no-op
  // and we suppress it.
  const trimmedBaseURL = baseURL.trim();
  const trimmedModel = model.trim();
  const dirty =
    trimmedBaseURL !== (initial?.base_url ?? "") ||
    trimmedModel !== (initial?.model ?? "");

  const onSaveEndpoint = async () => {
    if (!dirty) return;
    await save({
      provider_endpoints: {
        [meta.kind]: { base_url: trimmedBaseURL, model: trimmedModel },
      },
    });
  };

  const onSetDefault = async () => {
    await save({ llm_provider: meta.kind as ConfigUpdate["llm_provider"] });
  };

  const isDefault = cfg.llm_provider === meta.kind;
  // Windows users get the WSL2 banner above; suppressing the install
  // commands here avoids contradicting it (a user reading "use WSL2"
  // shouldn't see a bare `brew install ollama` snippet that won't run
  // in their host shell). They run the linux command inside WSL once
  // they're there.
  const cmdPlatform: "macos" | "linux" | undefined =
    hostPlatform === "macos"
      ? "macos"
      : hostPlatform === "linux"
        ? "linux"
        : undefined;
  const installCmd = cmdPlatform ? status?.install?.[cmdPlatform] : undefined;
  const startCmd = cmdPlatform ? status?.start?.[cmdPlatform] : undefined;

  return (
    <div
      data-testid={`local-llm-card-${meta.kind}`}
      style={{
        border: "1px solid var(--border-light)",
        borderRadius: 6,
        padding: 14,
        marginBottom: 14,
        background: "var(--bg-card)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 12,
          marginBottom: 6,
        }}
      >
        <div style={{ display: "flex", alignItems: "center" }}>
          <StatusDot status={status} />
          <strong style={{ fontSize: 14 }}>{meta.label}</strong>
          {isDefault && (
            <span
              style={{
                marginLeft: 10,
                fontSize: 11,
                padding: "1px 6px",
                background: "var(--accent-100, #eef)",
                color: "var(--accent-500, #44a)",
                borderRadius: 3,
              }}
            >
              Default
            </span>
          )}
          {status?.binary_version ? (
            <span
              style={{
                marginLeft: 8,
                fontSize: 11,
                color: "var(--text-tertiary)",
                fontFamily: "var(--font-mono)",
              }}
            >
              {status.binary_version}
            </span>
          ) : null}
        </div>
        {!isDefault && (
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={onSetDefault}
            data-testid={`local-llm-set-default-${meta.kind}`}
          >
            Set as default
          </button>
        )}
      </div>
      <p
        style={{
          fontSize: 12,
          color: "var(--text-tertiary)",
          margin: "4px 0 10px",
        }}
      >
        {meta.blurb}
      </p>

      {status?.windows_note ? (
        <div style={{ ...styles.banner, fontSize: 12 }}>
          <span style={{ fontSize: 14, flexShrink: 0 }}>{"⚠"}</span>
          <div>{status.windows_note}</div>
        </div>
      ) : null}

      <Field
        label="Base URL"
        hint={`WUPHF_${meta.kind.toUpperCase().replace(/-/g, "_")}_BASE_URL`}
      >
        <input
          style={styles.input}
          placeholder={status?.endpoint ?? "http://127.0.0.1:8080/v1"}
          value={baseURL}
          onChange={(e) => setBaseURL(e.target.value)}
          data-testid={`local-llm-base-url-${meta.kind}`}
        />
      </Field>
      <Field
        label="Model"
        hint={`WUPHF_${meta.kind.toUpperCase().replace(/-/g, "_")}_MODEL`}
      >
        <input
          style={styles.input}
          placeholder={status?.model ?? ""}
          value={model}
          onChange={(e) => setModel(e.target.value)}
          data-testid={`local-llm-model-${meta.kind}`}
        />
      </Field>
      <SaveButton label="Save endpoint" onSave={onSaveEndpoint} />

      {!status?.binary_installed && installCmd ? (
        <div
          style={{
            marginTop: 12,
            paddingTop: 10,
            borderTop: "1px dashed var(--border-light)",
          }}
        >
          <div
            style={{
              fontSize: 12,
              fontWeight: 500,
              marginBottom: 4,
              color: "var(--text-secondary)",
            }}
          >
            Install
          </div>
          <CommandRow command={installCmd} />
          {startCmd ? (
            <>
              <div
                style={{
                  fontSize: 12,
                  fontWeight: 500,
                  marginTop: 10,
                  marginBottom: 4,
                  color: "var(--text-secondary)",
                }}
              >
                Start
              </div>
              <CommandRow command={startCmd} />
            </>
          ) : null}
        </div>
      ) : null}
      {status?.binary_installed && !status.reachable && startCmd ? (
        <div
          style={{
            marginTop: 12,
            paddingTop: 10,
            borderTop: "1px dashed var(--border-light)",
          }}
        >
          <div
            style={{
              fontSize: 12,
              color: "var(--text-secondary)",
              marginBottom: 4,
            }}
          >
            Installed but the server isn't responding on{" "}
            <code style={{ fontFamily: "var(--font-mono)" }}>
              {status.endpoint}
            </code>
            . Start it from a terminal:
          </div>
          <CommandRow command={startCmd} />
        </div>
      ) : null}
      {(status?.notes ?? []).map((note) => (
        <p
          key={note}
          style={{
            fontSize: 11,
            color: "var(--text-tertiary)",
            marginTop: 8,
            marginBottom: 0,
          }}
        >
          {note}
        </p>
      ))}
    </div>
  );
}

function LocalLLMsSection({ cfg, save }: SectionProps) {
  const hostPlatform = detectHostPlatform();
  // refetch on the same cadence settings normally do; doctor probes are
  // cheap (~2s worst case for a wedged server) but we don't want to
  // hammer the broker every render.
  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["local-providers-status"],
    queryFn: getLocalProvidersStatus,
    refetchInterval: 30_000,
    staleTime: 5_000,
  });

  const byKind = new Map<string, LocalProviderStatus>();
  for (const s of data ?? []) byKind.set(s.kind, s);

  return (
    <div>
      <h2 style={styles.sectionTitle}>Local LLMs</h2>
      <p style={styles.sectionDesc}>
        Run wuphf agents through a model on your own machine — no cloud key
        required. Status indicators detect what's installed and what's
        responding; install commands are copy-paste only (we never run shell
        commands for you).
      </p>

      {hostPlatform === "windows" && (
        <div style={styles.banner}>
          <span style={{ fontSize: 14, flexShrink: 0 }}>{"⚠"}</span>
          <div>
            Local LLMs run best on macOS or Linux. Native Windows isn't
            supported; install your runtime inside WSL2 (Ubuntu) and the broker
            will detect it from there.
          </div>
        </div>
      )}

      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 8,
        }}
      >
        <div style={styles.groupTitle}>Available runtimes</div>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() => refetch()}
          disabled={isFetching}
          data-testid="local-llms-refresh"
        >
          <Refresh style={{ width: 14, height: 14, marginRight: 4 }} />
          {isFetching ? "Checking…" : "Recheck"}
        </button>
      </div>

      {isLoading ? (
        <div style={{ color: "var(--text-tertiary)", fontSize: 13 }}>
          Detecting installed runtimes…
        </div>
      ) : null}
      {error ? (
        <div style={{ color: "var(--danger-500, #c33)", fontSize: 13 }}>
          Failed to load status:{" "}
          {error instanceof Error ? error.message : String(error)}
        </div>
      ) : null}

      {!(isLoading || error) &&
        LOCAL_PROVIDERS.map((meta) => (
          <LocalProviderCard
            key={meta.kind}
            meta={meta}
            status={byKind.get(meta.kind)}
            cfg={cfg}
            save={save}
            hostPlatform={hostPlatform}
          />
        ))}
    </div>
  );
}

function CompanySection({ cfg, save }: SectionProps) {
  const [name, setName] = useState(cfg.company_name ?? "");
  const [description, setDescription] = useState(cfg.company_description ?? "");
  const [goals, setGoals] = useState(cfg.company_goals ?? "");
  const [size, setSize] = useState(cfg.company_size ?? "");
  const [priority, setPriority] = useState(cfg.company_priority ?? "");

  const onSave = () =>
    save({
      company_name: name,
      company_description: description,
      company_goals: goals,
      company_size: size,
      company_priority: priority,
    });

  return (
    <div>
      <h2 style={styles.sectionTitle}>Company</h2>
      <p style={styles.sectionDesc}>
        Organizational context injected into agent system prompts. The more you
        fill in, the better agents understand your business.
      </p>

      <Field label="Name" hint="Your company or project name">
        <input
          style={styles.input}
          placeholder="Acme Corp"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </Field>

      <Field label="Description" hint="One-liner about the business">
        <textarea
          style={styles.textarea}
          placeholder="What does your company do?"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>

      <Field label="Goals" hint="What the team is working toward">
        <textarea
          style={styles.textarea}
          placeholder="Current organizational goals"
          value={goals}
          onChange={(e) => setGoals(e.target.value)}
        />
      </Field>

      <Field label="Size" hint="Team or company size">
        <input
          style={styles.input}
          placeholder="e.g. 5, 50, 500"
          value={size}
          onChange={(e) => setSize(e.target.value)}
        />
      </Field>

      <Field label="Priority" hint="What matters most right now">
        <textarea
          style={styles.textarea}
          placeholder="Immediate priority focus"
          value={priority}
          onChange={(e) => setPriority(e.target.value)}
        />
      </Field>

      <SaveButton label="Save company info" onSave={onSave} />
    </div>
  );
}

interface KeyDef {
  field: keyof ConfigUpdate;
  flag: keyof ConfigSnapshot;
  label: string;
  placeholder: string;
  env: string;
}

const KEY_DEFS: KeyDef[] = [
  {
    field: "api_key",
    flag: "api_key_set",
    label: "Nex API Key",
    placeholder: "nex_...",
    env: "WUPHF_API_KEY",
  },
  {
    field: "anthropic_api_key",
    flag: "anthropic_key_set",
    label: "Anthropic",
    placeholder: "sk-ant-...",
    env: "ANTHROPIC_API_KEY",
  },
  {
    field: "openai_api_key",
    flag: "openai_key_set",
    label: "OpenAI",
    placeholder: "sk-...",
    env: "OPENAI_API_KEY",
  },
  {
    field: "gemini_api_key",
    flag: "gemini_key_set",
    label: "Gemini",
    placeholder: "AI...",
    env: "GEMINI_API_KEY",
  },
  {
    field: "minimax_api_key",
    flag: "minimax_key_set",
    label: "Minimax",
    placeholder: "mm-...",
    env: "MINIMAX_API_KEY",
  },
  {
    field: "one_api_key",
    flag: "one_key_set",
    label: "One (integration)",
    placeholder: "one_...",
    env: "ONE_SECRET",
  },
  {
    field: "composio_api_key",
    flag: "composio_key_set",
    label: "Composio",
    placeholder: "cmp_...",
    env: "COMPOSIO_API_KEY",
  },
  {
    field: "telegram_bot_token",
    flag: "telegram_token_set",
    label: "Telegram Bot",
    placeholder: "123456:ABC...",
    env: "WUPHF_TELEGRAM_BOT_TOKEN",
  },
];

function KeysSection({ cfg, save }: SectionProps) {
  const [values, setValues] = useState<Record<string, string>>({});

  const onSave = async () => {
    const entries = Object.entries(values).filter(([, v]) => v.trim() !== "");
    if (entries.length === 0) {
      showNotice("No keys entered. Leave blank to keep existing keys.", "info");
      return false;
    }
    const patch: ConfigUpdate = {};
    for (const [k, v] of entries) {
      (patch as Record<string, string>)[k] = v;
    }
    await save(patch);
    setValues({});
  };

  return (
    <div>
      <h2 style={styles.sectionTitle}>API Keys</h2>
      <p style={styles.sectionDesc}>
        Authentication credentials for external services. Keys are stored in
        your local config file and never transmitted to WUPHF servers. Enter a
        new value to update, or leave blank to keep the current key.
      </p>

      {KEY_DEFS.map((def) => (
        <Field key={def.field} label={def.label} hint={`Env: ${def.env}`}>
          <KeyField
            hasValue={Boolean(cfg[def.flag])}
            placeholder={def.placeholder}
            value={values[def.field] ?? ""}
            onChange={(v) => setValues((prev) => ({ ...prev, [def.field]: v }))}
          />
        </Field>
      ))}

      <SaveButton label="Save API keys" onSave={onSave} />
    </div>
  );
}

function IntegrationsSection({ cfg, save }: SectionProps) {
  const [actionProvider, setActionProvider] = useState<string>(
    cfg.action_provider ?? "auto",
  );
  const [gatewayUrl, setGatewayUrl] = useState(cfg.openclaw_gateway_url ?? "");
  const [openclawToken, setOpenclawToken] = useState("");

  const onSave = async () => {
    const patch: ConfigUpdate = {
      action_provider: actionProvider as ConfigUpdate["action_provider"],
    };
    if (gatewayUrl) patch.openclaw_gateway_url = gatewayUrl;
    if (openclawToken) patch.openclaw_token = openclawToken;
    await save(patch);
    setOpenclawToken("");
  };

  return (
    <div>
      <h2 style={styles.sectionTitle}>Integrations</h2>
      <p style={styles.sectionDesc}>
        External service connections and action providers.
      </p>

      <Field label="Action Provider" hint="External action routing">
        <select
          style={styles.input}
          value={actionProvider}
          onChange={(e) => setActionProvider(e.target.value)}
        >
          <option value="auto">Auto</option>
          <option value="one">One CLI</option>
          <option value="composio">Composio</option>
        </select>
      </Field>

      <div style={{ marginTop: 20 }}>
        <div style={styles.groupTitle}>OpenClaw</div>
        <Field label="Gateway URL" hint="WebSocket endpoint">
          <input
            style={{
              ...styles.input,
              fontFamily: "var(--font-mono)",
              fontSize: 12,
            }}
            placeholder="ws://127.0.0.1:18789"
            value={gatewayUrl}
            onChange={(e) => setGatewayUrl(e.target.value)}
          />
        </Field>
        <Field label="Token" hint="Gateway auth token">
          <KeyField
            hasValue={Boolean(cfg.openclaw_token_set)}
            placeholder="oc_..."
            value={openclawToken}
            onChange={setOpenclawToken}
          />
        </Field>
      </div>

      <div style={{ marginTop: 20 }}>
        <div style={styles.groupTitle}>Workspace</div>
        <Field label="Workspace ID" hint="Read-only">
          <input
            style={{ ...styles.input, opacity: 0.6, cursor: "default" }}
            readOnly={true}
            placeholder="(set via Nex registration)"
            value={cfg.workspace_id ?? ""}
          />
        </Field>
        <Field label="Workspace Slug" hint="Read-only">
          <input
            style={{ ...styles.input, opacity: 0.6, cursor: "default" }}
            readOnly={true}
            placeholder="(set via Nex registration)"
            value={cfg.workspace_slug ?? ""}
          />
        </Field>
      </div>

      <SaveButton label="Save integration settings" onSave={onSave} />
    </div>
  );
}

function IntervalsSection({ cfg, save }: SectionProps) {
  const [insights, setInsights] = useState(
    String(cfg.insights_poll_minutes ?? 15),
  );
  const [followUp, setFollowUp] = useState(
    String(cfg.task_follow_up_minutes ?? 60),
  );
  const [reminder, setReminder] = useState(
    String(cfg.task_reminder_minutes ?? 30),
  );
  const [recheck, setRecheck] = useState(
    String(cfg.task_recheck_minutes ?? 15),
  );

  const onSave = () =>
    save({
      insights_poll_minutes: parseInt(insights, 10) || 15,
      task_follow_up_minutes: parseInt(followUp, 10) || 60,
      task_reminder_minutes: parseInt(reminder, 10) || 30,
      task_recheck_minutes: parseInt(recheck, 10) || 15,
    });

  return (
    <div>
      <h2 style={styles.sectionTitle}>Polling Intervals</h2>
      <p style={styles.sectionDesc}>
        How often background processes check for updates. All values in minutes.
        Minimum 2 minutes.
      </p>

      <Field label="Insights" hint="Context graph polling">
        <input
          style={styles.input}
          type="number"
          min={2}
          placeholder="15"
          value={insights}
          onChange={(e) => setInsights(e.target.value)}
        />
      </Field>
      <Field label="Task Follow-up" hint="Post-completion check-in">
        <input
          style={styles.input}
          type="number"
          min={2}
          placeholder="60"
          value={followUp}
          onChange={(e) => setFollowUp(e.target.value)}
        />
      </Field>
      <Field label="Task Reminder" hint="Stalled task nudge">
        <input
          style={styles.input}
          type="number"
          min={2}
          placeholder="30"
          value={reminder}
          onChange={(e) => setReminder(e.target.value)}
        />
      </Field>
      <Field label="Task Recheck" hint="Progress re-evaluation">
        <input
          style={styles.input}
          type="number"
          min={2}
          placeholder="15"
          value={recheck}
          onChange={(e) => setRecheck(e.target.value)}
        />
      </Field>

      <SaveButton label="Save intervals" onSave={onSave} />
    </div>
  );
}

const CLI_FLAGS: [string, string][] = [
  ["--provider <name>", "LLM provider (claude-code, codex, opencode)"],
  ["--memory-backend <name>", "Memory backend (nex, gbrain, none)"],
  ["--blueprint <id>", "Operation blueprint for this run"],
  ["--tui", "Launch tmux TUI instead of web UI"],
  ["--web-port <port>", "Web UI port (default: 7891)"],
  ["--broker-port <port>", "Local broker port (default: 7890)"],
  ["--opus-ceo", "Upgrade CEO agent to Opus model"],
  ["--collab", "Collaborative mode (all agents see all messages)"],
  ["--1o1", "Direct 1:1 session with a single agent"],
  ["--unsafe", "Bypass agent permission checks (dev only)"],
  ["--no-nex", "Disable Nex for this session"],
  ["--no-open", "Skip auto-opening browser on launch"],
  ["--from-scratch", "Start without saved blueprint"],
  ["--threads-collapsed", "Start with threads collapsed"],
  ["--cmd <command>", "Run a slash command non-interactively"],
  ["--format <fmt>", "Output format (text, json)"],
  ["--api-key <key>", "Nex API key override"],
  ["--version", "Print version and exit"],
  ["--help-all", "Show all flags including internal ones"],
];

const ENV_VARS: [string, string][] = [
  ["WUPHF_LLM_PROVIDER", "LLM provider override"],
  ["WUPHF_MEMORY_BACKEND", "Memory backend override"],
  ["WUPHF_API_KEY", "Nex API key"],
  ["WUPHF_BROKER_PORT", "Broker port"],
  ["WUPHF_CONFIG_PATH", "Config file path override"],
  ["WUPHF_RUNTIME_HOME", "Runtime state directory"],
  ["WUPHF_NO_NEX", "Disable Nex (1/true/yes)"],
  ["WUPHF_START_FROM_SCRATCH", "Start without blueprint (1)"],
  ["WUPHF_ONE_ON_ONE", "Enable 1:1 mode (1)"],
  ["WUPHF_HEADLESS_PROVIDER", "Headless provider override"],
  ["WUPHF_INSIGHTS_INTERVAL_MINUTES", "Insights poll interval"],
  ["WUPHF_TASK_FOLLOWUP_MINUTES", "Task follow-up interval"],
  ["WUPHF_TASK_REMINDER_MINUTES", "Task reminder interval"],
  ["WUPHF_TASK_RECHECK_MINUTES", "Task recheck interval"],
];

function FlagsSection() {
  return (
    <div>
      <h2 style={styles.sectionTitle}>CLI Flags</h2>
      <p style={styles.sectionDesc}>
        All flags available when launching wuphf from the terminal. These are
        runtime-only and not persisted in the config file.
      </p>

      <table style={styles.table}>
        <thead>
          <tr>
            <th style={styles.th}>Flag</th>
            <th style={styles.th}>Description</th>
          </tr>
        </thead>
        <tbody>
          {CLI_FLAGS.map(([flag, desc]) => (
            <tr key={flag}>
              <td style={styles.tdFlag}>{flag}</td>
              <td style={styles.tdDesc}>{desc}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div style={{ marginTop: 24 }}>
        <div style={styles.groupTitle}>Environment Variables</div>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            lineHeight: 1.6,
            marginBottom: 12,
          }}
        >
          Settings resolve in order: CLI flag → environment variable → config
          file → default. Set these in your shell profile to override config
          file values.
        </p>
        <table style={styles.table}>
          <thead>
            <tr>
              <th style={styles.th}>Variable</th>
              <th style={styles.th}>Purpose</th>
            </tr>
          </thead>
          <tbody>
            {ENV_VARS.map(([v, p]) => (
              <tr key={v}>
                <td style={styles.tdFlag}>{v}</td>
                <td style={styles.tdDesc}>{p}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Danger Zone ────────────────────────────────────────────────────────

// dangerStyles lives next to the section because it's the only caller and the
// warning palette shouldn't bleed into the rest of the app's styling surface.
const dangerStyles = {
  card: (severity: "warn" | "critical") => ({
    marginBottom: 20,
    padding: 20,
    borderRadius: "var(--radius-md)",
    background: severity === "critical" ? "var(--red-bg)" : "var(--yellow-bg)",
  }),
  cardTitle: {
    display: "flex",
    alignItems: "center",
    gap: 8,
    fontSize: 15,
    fontWeight: 700,
    color: "var(--text)",
    marginBottom: 6,
  } as const,
  cardSubtitle: {
    fontSize: 13,
    color: "var(--text-secondary)",
    marginBottom: 14,
    lineHeight: 1.5,
  } as const,
  listLabel: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: "uppercase" as const,
    letterSpacing: "0.06em",
    color: "var(--text-tertiary)",
    marginTop: 8,
    marginBottom: 4,
  } as const,
  list: {
    margin: 0,
    paddingLeft: 20,
    fontSize: 12,
    lineHeight: 1.7,
    color: "var(--text-secondary)",
  } as const,
  button: (severity: "warn" | "critical") => ({
    marginTop: 16,
    padding: "9px 16px",
    fontSize: 13,
    fontWeight: 600,
    border: "none",
    borderRadius: "var(--radius-sm)",
    cursor: "pointer" as const,
    color: "#fff",
    background:
      severity === "critical"
        ? "var(--red, #e5484d)"
        : "var(--yellow, #e5a00d)",
    fontFamily: "var(--font-sans)",
  }),
};

type DangerAction = "reset" | "shred";

function DangerZoneSection() {
  const [open, setOpen] = useState<DangerAction | null>(null);
  const [busy, setBusy] = useState(false);
  const shred = useShredAction();

  const handleReset = async () => {
    setBusy(true);
    try {
      const result: WorkspaceWipeResult = await resetWorkspace();
      if (!result.ok) {
        showNotice(result.error || "Reset failed", "error");
        setBusy(false);
        return;
      }
      showNotice("Broker state cleared. Reloading…", "success");
      setTimeout(() => window.location.reload(), 400);
    } catch (err) {
      showNotice(err instanceof Error ? err.message : "Reset failed", "error");
      setBusy(false);
    }
  };

  const handleShred = async () => {
    setBusy(true);
    try {
      // Leave the modal mounted on failure so the user can retry without
      // having to reopen the Danger Zone and re-type the confirm phrase.
      // useShredAction surfaces the failure toast and never throws.
      const ok = await shred();
      if (ok) setOpen(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <div style={styles.sectionTitle}>Danger Zone</div>
      <div style={styles.sectionDesc}>
        Irreversible operations on this workspace. Reset reloads the current
        broker. Shred wipes local workspace history and reopens onboarding in
        the running web UI.
      </div>

      {/* RESET — narrow: broker runtime state only */}
      <div style={dangerStyles.card("warn")}>
        <div style={dangerStyles.cardTitle}>
          <Refresh width={16} height={16} />
          <span>Reset broker state</span>
        </div>
        <div style={dangerStyles.cardSubtitle}>
          Use this when something is stuck — an agent wedged, the queue won't
          drain, messages stop flowing — and you want a clean restart without
          losing your team or work.
        </div>
        <div style={dangerStyles.listLabel}>Clears</div>
        <ul style={dangerStyles.list}>
          <li>
            Broker runtime state (<code>~/.wuphf/team/broker-state.json</code>)
          </li>
          <li>Last-good in-memory snapshot</li>
        </ul>
        <div style={dangerStyles.listLabel}>Preserved</div>
        <ul style={dangerStyles.list}>
          <li>Your team roster, company identity, tasks, workflows</li>
          <li>All on-disk history (logs, sessions, artifacts)</li>
          <li>API keys and config</li>
        </ul>
        <button
          type="button"
          style={dangerStyles.button("warn")}
          onClick={() => setOpen("reset")}
          disabled={busy}
        >
          Reset broker state…
        </button>
      </div>

      {/* SHRED — full wipe */}
      <div style={dangerStyles.card("critical")}>
        <div style={dangerStyles.cardTitle}>
          <WarningTriangle width={16} height={16} />
          <span>Shred workspace</span>
        </div>
        <div style={dangerStyles.cardSubtitle}>
          <ShredCardSubtitle />
        </div>
        <div style={dangerStyles.listLabel}>Deletes</div>
        <ul style={dangerStyles.list}>
          <ShredDeletionsList />
        </ul>
        <div style={dangerStyles.listLabel}>Preserved</div>
        <ul style={dangerStyles.list}>
          <ShredPreservationList />
        </ul>
        <button
          type="button"
          style={dangerStyles.button("critical")}
          onClick={() => setOpen("shred")}
          disabled={busy}
        >
          Shred workspace…
        </button>
      </div>

      {open === "reset" && (
        <WipeModal
          title="Reset broker state?"
          severity="warn"
          intro={
            <>
              This clears the broker's on-disk runtime state and reboots the
              office from a clean slate. Your team, company, tasks, and
              workflows are all kept. If this doesn't unblock things, try{" "}
              <strong>Shred workspace</strong> instead.
            </>
          }
          confirmLabel="Reset broker state"
          busy={busy}
          onConfirm={handleReset}
          onCancel={() => setOpen(null)}
        />
      )}

      {open === "shred" && (
        <WipeModal
          title="Shred this workspace?"
          severity="critical"
          intro={<ShredWarningCopy />}
          confirmLabel="Shred workspace"
          busy={busy}
          onConfirm={handleShred}
          onCancel={() => setOpen(null)}
        />
      )}
    </div>
  );
}

// ─── Main component ─────────────────────────────────────────────────────

export function SettingsApp() {
  const [section, setSection] = useState<SectionId>("general");
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 10_000,
  });

  const saveMutation = useMutation({
    mutationFn: (patch: ConfigUpdate) => updateConfig(patch),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["config"] });
      showNotice("Settings saved.", "success");
    },
    onError: (err: unknown) => {
      const message =
        err instanceof Error ? err.message : "Failed to save settings";
      showNotice(message, "error");
    },
  });

  // Reset section state when data changes so form values pick up latest server state
  const [dataKey, setDataKey] = useState(0);
  useEffect(() => {
    setDataKey((k) => k + 1);
  }, []);

  const save = async (patch: ConfigUpdate) => {
    await saveMutation.mutateAsync(patch);
  };

  if (isLoading) {
    return (
      <div
        style={{
          padding: 40,
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Loading settings...
      </div>
    );
  }

  if (error || !data) {
    return (
      <div
        style={{
          padding: 40,
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Failed to load settings:{" "}
        {error instanceof Error ? error.message : String(error)}
      </div>
    );
  }

  return (
    <div style={styles.shell}>
      <nav style={styles.nav}>
        {SECTION_GROUPS.map((group) => (
          <div key={group.label}>
            <p style={styles.navGroupLabel}>{group.label}</p>
            {group.items.map((sec) => {
              const { Icon } = sec;
              return (
                <button
                  type="button"
                  key={sec.id}
                  style={styles.navItem(sec.id === section)}
                  onClick={() => setSection(sec.id)}
                >
                  <Icon style={styles.navIcon} />
                  <span>{sec.name}</span>
                </button>
              );
            })}
          </div>
        ))}
      </nav>
      <div style={styles.body} key={dataKey}>
        {section === "general" && <GeneralSection cfg={data} save={save} />}
        {section === "local-llms" && (
          <LocalLLMsSection cfg={data} save={save} />
        )}
        {section === "image-gen" && <ImageGenSection />}
        {section === "company" && <CompanySection cfg={data} save={save} />}
        {section === "keys" && <KeysSection cfg={data} save={save} />}
        {section === "integrations" && (
          <IntegrationsSection cfg={data} save={save} />
        )}
        {section === "intervals" && <IntervalsSection cfg={data} save={save} />}
        {section === "flags" && <FlagsSection />}
        {section === "danger" && <DangerZoneSection />}
      </div>
    </div>
  );
}
