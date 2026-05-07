import { useCallback, useMemo, useState } from "react";

import { useSessionRole } from "../../hooks/useSessionRole";

const STORAGE_PREFIX = "wuphf:team-welcome-dismissed:";

// Pick the most stable identifier the broker exposes for this joiner
// session. invite_id is the ideal key (one per invite use), but if the
// broker has not surfaced it on /humans/me yet we degrade gracefully.
function dismissalKey(human: {
  invite_id?: string;
  id?: string;
  human_slug?: string;
}): string | null {
  const id = human.invite_id ?? human.id ?? human.human_slug;
  if (!id) return null;
  return `${STORAGE_PREFIX}${id}`;
}

function hasDismissed(key: string): boolean {
  try {
    return window.localStorage.getItem(key) === "1";
  } catch {
    // localStorage can throw in private-mode Safari or when quota is full.
    // Treat the welcome card as not-yet-dismissed so the joiner still sees
    // it — losing the suppression is preferable to crashing the shell.
    return false;
  }
}

function markDismissed(key: string): void {
  try {
    window.localStorage.setItem(key, "1");
  } catch {
    // Same fallback as hasDismissed: silently accept that the dismiss will
    // not survive a refresh, rather than block the close action.
  }
}

export function TeamMemberWelcome() {
  const { role, human, hostDisplayName } = useSessionRole();
  const key = useMemo(() => (human ? dismissalKey(human) : null), [human]);
  const [dismissedThisRender, setDismissedThisRender] = useState(false);

  const onDismiss = useCallback(() => {
    if (key) markDismissed(key);
    setDismissedThisRender(true);
  }, [key]);

  if (role !== "member" || !human) return null;
  if (dismissedThisRender) return null;
  if (key && hasDismissed(key)) return null;

  const displayName = human.display_name?.trim() || "team member";
  // Possessive form: "Sam" → "Sam's", "Chris" → "Chris's". Names ending in
  // "s" still get an apostrophe-s — Strunk & White, and easier to read than
  // "Chris' office" which trips up scan-readers on a small card.
  const officeLabel = hostDisplayName
    ? `${hostDisplayName}'s office`
    : "this office";

  return (
    <aside
      className="team-welcome"
      role="status"
      aria-label="Team member session welcome"
    >
      <div className="team-welcome-body">
        <p className="team-welcome-title">
          You&apos;re in. You joined {officeLabel} as{" "}
          <strong>{displayName}</strong>.
        </p>
        <p className="team-welcome-copy">
          This is a scoped team-member browser session. The host can revoke
          access at any time from Access &amp; Health.
        </p>
      </div>
      <button
        type="button"
        className="team-welcome-dismiss"
        onClick={onDismiss}
        aria-label="Dismiss welcome message"
      >
        Got it
      </button>
    </aside>
  );
}
