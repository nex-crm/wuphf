import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";
import { RequestItem } from "./RequestItem";

describe("<RequestItem>", () => {
  it("collects text before answering legacy direct-answer interviews", () => {
    const request: AgentRequest = {
      id: "request-200",
      from: "research",
      question: "What should we ask next?",
      title: "Human interview",
      blocking: false,
      status: "pending",
      options: [{ id: "answer_directly", label: "Answer directly" }],
      kind: "interview",
    };
    const onAnswer = vi.fn();

    render(
      <RequestItem request={request} isPending={true} onAnswer={onAnswer} />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Answer directly/i }));
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Ask whether they already use a workaround." },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Send as Answer directly/i }),
    );

    expect(onAnswer).toHaveBeenCalledWith(
      "answer_directly",
      "Ask whether they already use a workaround.",
    );
  });
});
