// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal, image, or routed-control keyboard path; preserving current interaction model.
import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import {
  get,
  getConfig,
  getLocalProvidersStatus,
  type LLMProvider,
  updateConfig,
} from "../../api/client";
import {
  configuredConnectedRuntimeProviders,
  type RuntimeProviderOption,
} from "../../lib/runtimeProviders";
import type { PrereqResult } from "../onboarding/runtimes";
import { confirm } from "./ConfirmDialog";
import { showNotice } from "./Toast";

let requestOpen: (() => void) | null = null;

/** Imperatively open the provider switcher from anywhere. */
export function openProviderSwitcher() {
  if (!requestOpen) {
    showNotice("Provider switcher is not available right now.", "error");
    return;
  }
  requestOpen();
}

export function ProviderSwitcherHost() {
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState<LLMProvider | null>(null);
  const [providers, setProviders] = useState<RuntimeProviderOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [pending, setPending] = useState<LLMProvider | null>(null);
  const queryClient = useQueryClient();

  useEffect(() => {
    requestOpen = () => {
      setOpen(true);
      setLoading(true);
      Promise.all([
        getConfig(),
        get<{ prereqs?: PrereqResult[] } | PrereqResult[]>(
          "/onboarding/prereqs",
        ),
        getLocalProvidersStatus(),
      ])
        .then(([cfg, prereqsPayload, localStatuses]) => {
          const prereqs = Array.isArray(prereqsPayload)
            ? prereqsPayload
            : (prereqsPayload.prereqs ?? []);
          const available = configuredConnectedRuntimeProviders(cfg, {
            prereqs,
            localStatuses,
          });
          setProviders(available);
          setCurrent(
            available.find((option) => option.id === cfg.llm_provider)?.id ??
              available[0]?.id ??
              null,
          );
        })
        .catch(() => {
          setProviders([]);
          setCurrent(null);
        })
        .finally(() => setLoading(false));
    };
    return () => {
      if (requestOpen !== null) requestOpen = null;
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  if (!open) return null;

  async function switchTo(p: RuntimeProviderOption) {
    if (!current || p.id === current) return;
    confirm({
      title: "Switch runtime provider?",
      message: `Agents will be restarted on ${p.label}.`,
      confirmLabel: "Switch",
      onConfirm: async () => {
        setPending(p.id);
        try {
          await updateConfig({
            llm_provider: p.id,
            llm_provider_priority: [
              p.id,
              ...providers
                .filter((option) => option.id !== p.id)
                .map((option) => option.id),
            ],
          });
          await queryClient.invalidateQueries({ queryKey: ["config"] });
          await queryClient.invalidateQueries({ queryKey: ["health"] });
          setCurrent(p.id);
          showNotice(`Provider switched to ${p.label}`, "success");
          setOpen(false);
        } catch (err: unknown) {
          const message = err instanceof Error ? err.message : "Switch failed";
          showNotice(`Switch failed: ${message}`, "error");
        } finally {
          setPending(null);
        }
      },
    });
  }

  return (
    <div
      className="provider-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) setOpen(false);
      }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="provider-title"
    >
      <div className="provider-panel card">
        <h3 id="provider-title" className="provider-title">
          Runtime provider
        </h3>
        {loading ? (
          <p className="provider-loading">Loading current provider...</p>
        ) : providers.length === 0 ? (
          <p className="provider-loading">
            No configured provider is connected. Check Settings.
          </p>
        ) : (
          <div className="provider-options">
            {providers.map((p) => {
              const isActive = current === p.id;
              const isPending = pending === p.id;
              return (
                <button
                  key={p.id}
                  type="button"
                  className={`provider-option${isActive ? " active" : ""}`}
                  onClick={() => switchTo(p)}
                  disabled={isActive || isPending}
                >
                  <div className="provider-option-text">
                    <div className="provider-option-name">{p.label}</div>
                    <div className="provider-option-desc">{p.desc}</div>
                  </div>
                  {isActive && (
                    <span className="provider-option-check">{"\u2713"}</span>
                  )}
                  {isPending && (
                    <span className="provider-option-check">...</span>
                  )}
                </button>
              );
            })}
          </div>
        )}
        <div className="provider-footer">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={() => setOpen(false)}
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
