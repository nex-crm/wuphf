import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import type { EmbeddingOptions } from "../../../api/knowledge";
import "../../../styles/onboarding-wizard.css";
import { EmbeddingChoiceView } from "./EmbeddingChoice";

/**
 * Stories for the wiki step's "Power semantic memory" section. The view is
 * presentational, so each state is driven purely by the `options` it receives.
 * A thin stateful harness keeps the key input interactive without mounting the
 * fetch/save container. Check each story in Nex Light, Nex Dark, and Noir Gold.
 */

const BASE: EmbeddingOptions = {
  gbrain_installed: true,
  openai_key_set: false,
  ollama_available: false,
  ollama_model: "",
  active_embedder: "keyword",
  embedding_available: false,
  recommended: "openai",
  install_state: "idle",
  install_progress: "",
  install_error: "",
};

interface HarnessProps {
  options: EmbeddingOptions;
  saving?: boolean;
  saveError?: string | null;
  /** Pre-select the local (Ollama) path so the install affordance shows. */
  ollamaChosen?: boolean;
  /** Reflect the install POST being in flight (CTA / retry disabled). */
  installBusy?: boolean;
}

/** Wraps the view with local key + save-error state so the input is live. */
function Harness({
  options,
  saving = false,
  saveError = null,
  ollamaChosen = false,
  installBusy = false,
}: HarnessProps) {
  const [keyValue, setKeyValue] = useState("");
  const [chosen, setChosen] = useState(ollamaChosen);
  return (
    <div style={{ maxWidth: 520, padding: "var(--space-4)" }}>
      <EmbeddingChoiceView
        options={options}
        keyValue={keyValue}
        onKeyChange={setKeyValue}
        onSaveKey={() => undefined}
        saving={saving}
        saveError={saveError}
        ollamaChosen={chosen}
        onChooseOllama={() => setChosen(true)}
        installBusy={installBusy}
        onInstallGbrain={() => undefined}
      />
    </div>
  );
}

const meta: Meta<typeof Harness> = {
  title: "Onboarding / EmbeddingChoice",
  component: Harness,
};

export default meta;
type Story = StoryObj<typeof Harness>;

/** No key yet, but a local Ollama model is reachable — the enabled alternative. */
export const NoKeyOllamaAvailable: Story = {
  args: {
    options: {
      ...BASE,
      ollama_available: true,
      ollama_model: "nomic-embed-text",
      active_embedder: "ollama",
      embedding_available: true,
    },
  },
};

/** No key and no Ollama — keyword is the instant default, Ollama shows setup. */
export const NoKeyNoOllama: Story = {
  args: {
    options: { ...BASE },
  },
};

/** Save round-trip in flight: the button shows its saving label and is disabled. */
export const SavingKey: Story = {
  args: {
    options: { ...BASE },
    saving: true,
  },
};

/** A save that failed: inline, non-blocking error under the key field. */
export const SaveError: Story = {
  args: {
    options: { ...BASE },
    saveError:
      "We could not save that key. Check it and try again, or continue on keyword search.",
  },
};

/** Key is set: the section collapses to the success state, alternatives hidden. */
export const KeySet: Story = {
  args: {
    options: {
      ...BASE,
      openai_key_set: true,
      active_embedder: "openai",
      embedding_available: true,
    },
  },
};

/**
 * No gbrain index yet. These stories drive the install affordance, which only
 * appears when the user wants a semantic path and gbrain_installed is false.
 */
const NOT_INSTALLED: EmbeddingOptions = {
  ...BASE,
  gbrain_installed: false,
  recommended: "openai",
};

/** User picked the local path; gbrain is not installed, so the consent shows. */
export const InstallNeedsSetup: Story = {
  args: {
    options: { ...NOT_INSTALLED },
    ollamaChosen: true,
  },
};

/** Install running: a compact progress row with the latest line and a spinner. */
export const Installing: Story = {
  args: {
    options: {
      ...NOT_INSTALLED,
      install_state: "installing",
      install_progress: "Installing gbrain via Bun (this can take a minute).",
    },
    ollamaChosen: true,
  },
};

/** Install finished before gbrain_installed flipped: the ready line shows. */
export const InstalledAfterSetup: Story = {
  args: {
    options: {
      ...NOT_INSTALLED,
      install_state: "installed",
    },
    ollamaChosen: true,
  },
};

/** Install failed: the reason, the keyword fallback, and a retry button. */
export const InstallError: Story = {
  args: {
    options: {
      ...NOT_INSTALLED,
      install_state: "error",
      install_error:
        "Bun could not be bootstrapped. Check your network and disk space.",
    },
    ollamaChosen: true,
  },
};
