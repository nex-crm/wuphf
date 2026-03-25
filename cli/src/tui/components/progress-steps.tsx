import React from "react";
import { Box, Text } from "ink";
import { useSpinner } from "./spinner.js";

// ── Types ───────────────────────────────────────────────────────────

export type StepStatus = "done" | "active" | "pending";

export interface Step {
  label: string;
  status: StepStatus;
}

export interface ProgressStepsProps {
  steps: Step[];
  /** Layout direction (default: "column") */
  direction?: "row" | "column";
}

// ── Step icon component ─────────────────────────────────────────────

function StepIcon({ status }: { status: StepStatus }): React.JSX.Element {
  const frame = useSpinner();

  if (status === "done") {
    return <Text color="green">{"✓"}</Text>;
  }
  if (status === "active") {
    return <Text color="cyan">{frame}</Text>;
  }
  // pending
  return <Text dimColor>{"○"}</Text>;
}

// ── Component ───────────────────────────────────────────────────────

export function ProgressSteps({
  steps,
  direction = "column",
}: ProgressStepsProps): React.JSX.Element {
  return (
    <Box flexDirection={direction} gap={direction === "row" ? 2 : 0}>
      {steps.map((step, i) => (
        <Box key={i} gap={1}>
          <StepIcon status={step.status} />
          <Text
            color={step.status === "active" ? "white" : undefined}
            dimColor={step.status === "pending"}
            bold={step.status === "active"}
          >
            {step.label}
          </Text>
        </Box>
      ))}
    </Box>
  );
}

export default ProgressSteps;
