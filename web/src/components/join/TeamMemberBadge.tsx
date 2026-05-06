import { useSessionRole } from "../../hooks/useSessionRole";

export function TeamMemberBadge() {
  const { role } = useSessionRole();
  if (role !== "member") return null;
  return (
    <span
      className="team-member-badge"
      role="status"
      aria-label="Team-member session"
      title="You are signed in as a team member of this office. The host can revoke your session at any time."
    >
      team-member session
    </span>
  );
}
