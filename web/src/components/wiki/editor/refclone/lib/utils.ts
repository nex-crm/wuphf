import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// Allowlist of link hrefs the editor will turn into clickable anchors. Permits
// http(s), mailto, tel, root-relative, dotted-relative, and fragment targets;
// rejects dangerous schemes (javascript:, data:, vbscript:) so a user-supplied
// URL cannot become a click-to-execute XSS via the link/media inserts.
const SAFE_LINK_HREF = /^(https?:\/\/|mailto:|tel:|\/|\.{1,2}\/|#)/i;

export function isSafeLinkHref(href: string): boolean {
  return SAFE_LINK_HREF.test(href.trim());
}
