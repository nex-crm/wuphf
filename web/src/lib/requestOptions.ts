import type { AgentRequest, InterviewOption } from "../api/client";

export function requestOptionNeedsText(
  request: AgentRequest,
  option: InterviewOption,
): boolean {
  return Boolean(
    option.requires_text ||
      (request.kind === "interview" && option.id === "answer_directly"),
  );
}

export function requestOptionTextHint(
  request: AgentRequest,
  option: InterviewOption,
): string {
  if (option.text_hint) return option.text_hint;
  if (request.kind === "interview" && option.id === "answer_directly") {
    return "Type your answer for the team.";
  }
  return "Type your answer...";
}
