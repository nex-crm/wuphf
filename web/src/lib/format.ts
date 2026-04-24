/** Format a timestamp into a relative date label (Today, Yesterday, etc.) */
export function formatDateLabel(ts: string): string {
  const d = new Date(ts);
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const target = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const diff = today.getTime() - target.getTime();
  if (diff === 0) return "Today";
  if (diff === 86400000) return "Yesterday";
  return d.toLocaleDateString([], {
    weekday: "short",
    month: "short",
    day: "numeric",
  });
}

/** Format a timestamp into a human-friendly relative string. */
export function formatRelativeTime(ts: string): string {
  if (!ts) return "";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  const now = new Date();
  const diff = d.getTime() - now.getTime();
  const absDiff = Math.abs(diff);
  const minutes = Math.floor(absDiff / 60000);
  const hours = Math.floor(absDiff / 3600000);
  const days = Math.floor(absDiff / 86400000);

  if (diff > 0) {
    if (minutes < 1) return "now";
    if (minutes < 60) return `in ${minutes}min`;
    if (hours < 24) return `in ${hours}h`;
    if (days === 1) return "tomorrow";
    return `in ${days} days`;
  }
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}min ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days === 1) return "yesterday";
  return `${days} days ago`;
}

/** Format a timestamp as HH:MM. */
export function formatTime(ts: string): string {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

/** Format USD cost. */
export function formatUSD(n: number): string {
  return `$${(n || 0).toFixed(4)}`;
}

/** Format token count with k/M suffix. */
export function formatTokens(n: number): string {
  if (!n) return "0";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}
