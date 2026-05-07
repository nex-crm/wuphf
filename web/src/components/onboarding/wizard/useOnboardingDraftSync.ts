// useOnboardingDraftSync — debounced persistence of the non-secret
// wizard draft. Extracted from Wizard.tsx to keep the parent
// component's cognitive complexity under the repo's biome budget.
//
// The hook does one thing: schedules a setTimeout to write the
// draftable subset of state to localStorage 300ms after any change.
// API keys and other secrets are never read here — saveDraft only
// persists the OnboardingDraft shape.

import { useEffect } from "react";

import {
  type DraftableWizardState,
  extractDraftableState,
  saveDraft,
} from "./onboardingDraft";

const SAVE_DEBOUNCE_MS = 300;

// Skip the save when the user has not touched anything yet — avoids
// writing an empty placeholder draft on first welcome-step render that
// would then flag a "resumeable session" the user never started.
function isPristine(state: DraftableWizardState): boolean {
  return (
    state.step === "welcome" &&
    state.selectedBlueprint === null &&
    state.company === "" &&
    state.description === "" &&
    state.priority === "" &&
    state.runtimePriority.length === 0 &&
    state.localProvider === "" &&
    state.selectedTaskTemplate === null &&
    state.taskText === ""
  );
}

export function useOnboardingDraftSync(state: DraftableWizardState): void {
  const {
    step,
    selectedBlueprint,
    company,
    description,
    priority,
    runtimePriority,
    localProvider,
    selectedTaskTemplate,
    taskText,
  } = state;

  useEffect(() => {
    const next: DraftableWizardState = {
      step,
      selectedBlueprint,
      company,
      description,
      priority,
      runtimePriority,
      localProvider,
      selectedTaskTemplate,
      taskText,
    };
    if (isPristine(next)) return;
    const handle = window.setTimeout(() => {
      saveDraft(extractDraftableState(next));
    }, SAVE_DEBOUNCE_MS);
    return () => {
      window.clearTimeout(handle);
    };
  }, [
    step,
    selectedBlueprint,
    company,
    description,
    priority,
    runtimePriority,
    localProvider,
    selectedTaskTemplate,
    taskText,
  ]);
}
