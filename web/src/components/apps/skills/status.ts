import type { SkillStatus } from "../../../api/client";

export const STATUS_BADGE_CLASS: Record<SkillStatus, string> = {
  active: "badge badge-green",
  proposed: "badge badge-yellow",
  disabled: "badge badge-neutral",
  archived: "badge badge-muted",
};
