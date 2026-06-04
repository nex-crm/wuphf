import type { ReactElement } from "react";

// Brand glyphs for each integration. Stored as inline SVG so they stay crisp
// and do not depend on remote asset requests in the settings surface.

const SIZE = 28;

export function TelegramLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 240 240"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <defs>
        <linearGradient id="telegram-bg" x1="120" y1="240" x2="120" y2="0">
          <stop offset="0" stopColor="#1D93D2" />
          <stop offset="1" stopColor="#38B0E3" />
        </linearGradient>
      </defs>
      <circle cx="120" cy="120" r="120" fill="url(#telegram-bg)" />
      <path
        d="M81.229 128.772l14.237 39.406s1.78 3.687 3.686 3.687 30.255-29.492 30.255-29.492l31.525-60.89-79.195 37.117z"
        fill="#C8DAEA"
      />
      <path
        d="M100.106 138.878l-2.733 29.046s-1.144 8.9 7.754 0 17.415-15.763 17.415-15.763"
        fill="#A9C6D8"
      />
      <path
        d="M81.486 130.178l-29.286-9.542s-3.5-1.42-2.373-4.64c.232-.664.7-1.229 2.1-2.2 6.489-4.523 120.106-45.36 120.106-45.36s3.208-1.081 5.1-.362a2.766 2.766 0 011.885 2.055c.221.767.287 1.642.254 2.585-.009.752-.1 1.449-.169 2.542-.692 11.165-21.4 94.493-21.4 94.493s-1.239 4.876-5.678 5.043a8.13 8.13 0 01-5.925-2.292c-8.711-7.493-38.819-27.727-45.472-32.177a1.27 1.27 0 01-.546-.9c-.093-.469.417-1.05.417-1.05s52.426-46.6 53.821-51.492c.108-.379-.3-.566-.848-.4-3.482 1.281-63.844 39.4-70.506 43.607a3.21 3.21 0 01-1.48.09z"
        fill="#FFFFFF"
      />
    </svg>
  );
}

export function OpenClawLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 120 120"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <defs>
        <linearGradient id="openclaw-bg" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="#FF4D4D" />
          <stop offset="100%" stopColor="#991B1B" />
        </linearGradient>
      </defs>
      <path
        d="M60 10C30 10 15 35 15 55c0 20 15 40 30 45v10h10v-10s5 2 10 0v10h10v-10c15-5 30-25 30-45 0-20-15-45-45-45z"
        fill="url(#openclaw-bg)"
      />
      <path
        d="M20 45C5 40 0 50 5 60c5 10 15 5 20-5 3-7 0-10-5-10zM100 45c15-5 20 5 15 15s-15 5-20-5c-3-7 0-10 5-10z"
        fill="url(#openclaw-bg)"
      />
      <path
        d="M45 15Q35 5 30 8M75 15Q85 5 90 8"
        stroke="#FF4D4D"
        strokeWidth="3"
        strokeLinecap="round"
      />
      <circle cx="45" cy="35" r="6" fill="#050810" />
      <circle cx="75" cy="35" r="6" fill="#050810" />
      <circle cx="46" cy="34" r="2.5" fill="#00E5CC" />
      <circle cx="76" cy="34" r="2.5" fill="#00E5CC" />
    </svg>
  );
}

export function HermesLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="12" fill="#FFB02E" />
      <path d="M18 15h8v14h16V15h8v34h-8V36H26v13h-8z" fill="#090A0C" />
    </svg>
  );
}

export function GmailLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="var(--bg-card)" />
      <path d="M12 22v27h9V29.2L32 37l11-7.8V49h9V22l-20 15z" fill="#EA4335" />
      <path d="M12 22l20 15 20-15v-7L32 30 12 15z" fill="#FBBC05" />
      <path d="M12 22v27h9V29.2z" fill="#34A853" />
      <path d="M43 29.2V49h9V22z" fill="#4285F4" />
    </svg>
  );
}

export function SlackLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="var(--bg-card)" />
      <path
        d="M24 14a6 6 0 016 6v14a6 6 0 11-12 0V20a6 6 0 016-6z"
        fill="#36C5F0"
      />
      <path
        d="M14 40a6 6 0 016-6h14a6 6 0 110 12H20a6 6 0 01-6-6z"
        fill="#2EB67D"
      />
      <path
        d="M40 50a6 6 0 01-6-6V30a6 6 0 1112 0v14a6 6 0 01-6 6z"
        fill="#ECB22E"
      />
      <path
        d="M50 24a6 6 0 01-6 6H30a6 6 0 110-12h14a6 6 0 016 6z"
        fill="#E01E5A"
      />
      <circle cx="24" cy="40" r="6" fill="#2EB67D" />
      <circle cx="40" cy="24" r="6" fill="#E01E5A" />
    </svg>
  );
}

export function GitHubLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="var(--bg-card)" />
      <path
        fill="currentColor"
        d="M32 13.5c-10.5 0-19 8.5-19 19 0 8.4 5.5 15.6 13.1 18.1 1 .2 1.3-.4 1.3-.9v-3.3c-5.3 1.2-6.4-2.3-6.4-2.3-.9-2.2-2.1-2.8-2.1-2.8-1.7-1.2.1-1.2.1-1.2 1.9.1 3 2 3 2 1.7 2.9 4.5 2.1 5.5 1.6.2-1.2.7-2.1 1.2-2.6-4.3-.5-8.8-2.1-8.8-9.5 0-2.1.8-3.8 2-5.2-.2-.5-.9-2.5.2-5.1 0 0 1.6-.5 5.3 2a18.4 18.4 0 019.6 0c3.7-2.5 5.3-2 5.3-2 1.1 2.6.4 4.6.2 5.1 1.2 1.4 2 3.1 2 5.2 0 7.4-4.5 9-8.8 9.5.7.6 1.3 1.8 1.3 3.6v5.3c0 .5.4 1.1 1.3.9A19 19 0 0051 32.5c0-10.5-8.5-19-19-19z"
      />
    </svg>
  );
}

export function ToolkitBrandLogo({
  platform,
}: {
  platform: string;
}): ReactElement | null {
  switch (platform.trim().toLowerCase()) {
    case "gmail":
      return <GmailLogo />;
    case "slack":
      return <SlackLogo />;
    case "github":
      return <GitHubLogo />;
    default:
      return null;
  }
}

// Generic glyph used as a fallback when an integration has no branded mark.
// Same dimensions as the branded marks so list rows stay aligned.
export function GenericIntegrationLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="var(--bg-warm)" />
      <path
        d="M22 26h20v4l-6 6 6 6v4H22v-4l6-6-6-6z"
        fill="none"
        stroke="var(--text-secondary)"
        strokeWidth="2.5"
        strokeLinejoin="round"
      />
    </svg>
  );
}
