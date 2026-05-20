import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";
import { RequestItem } from "./RequestItem";

describe("<RequestItem>", () => {
  function makeInterviewRequest(
    id: string,
    options: AgentRequest["options"] = [
      { id: "answer_directly", label: "Answer directly" },
    ],
  ): AgentRequest {
    return {
      id,
      from: "research",
      question: "What should we ask next?",
      title: "Human interview",
      blocking: false,
      status: "pending",
      options,
      kind: "interview",
    };
  }

  it("collects text before answering legacy direct-answer interviews", () => {
    const request = makeInterviewRequest("request-200");
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

  it("resets text mode when the request changes", () => {
    const firstRequest = makeInterviewRequest("request-201");
    const secondRequest = makeInterviewRequest("request-202", [
      { id: "accept", label: "Accept" },
    ]);
    const onAnswer = vi.fn();

    const { rerender } = render(
      <RequestItem
        request={firstRequest}
        isPending={true}
        onAnswer={onAnswer}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Answer directly/i }));
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Stale text from the first request." },
    });

    rerender(
      <RequestItem
        request={secondRequest}
        isPending={true}
        onAnswer={onAnswer}
      />,
    );

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Accept/i }));

    expect(onAnswer).toHaveBeenCalledTimes(1);
    expect(onAnswer).toHaveBeenCalledWith("accept");
  });
});
