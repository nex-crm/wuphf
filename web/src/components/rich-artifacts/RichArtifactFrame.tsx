import "../../styles/rich-artifacts.css";

const SANDBOX_CSP = [
  "default-src 'none'",
  "style-src 'unsafe-inline'",
  "script-src 'unsafe-inline'",
  "img-src data: blob:",
  "media-src data: blob:",
  "font-src data:",
  "connect-src 'none'",
  "worker-src 'none'",
  "frame-src 'none'",
  "object-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
].join("; ");

function withSandboxCsp(html: string): string {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${SANDBOX_CSP}">`;
  return `<!doctype html><html><head>${meta}</head><body>${html}</body></html>`;
}

function frameKey(title: string, html: string): string {
  let hash = 5381;
  const input = `${title}\u0000${html}`;
  for (let i = 0; i < input.length; i += 1) {
    hash = (hash * 33) ^ input.charCodeAt(i);
  }
  return `rich-artifact-${hash >>> 0}`;
}

interface RichArtifactFrameProps {
  title: string;
  html: string;
}

export default function RichArtifactFrame({
  title,
  html,
}: RichArtifactFrameProps) {
  return (
    <iframe
      key={frameKey(title, html)}
      className="rich-artifact-frame"
      title={title}
      sandbox="allow-scripts"
      srcDoc={withSandboxCsp(html)}
    />
  );
}
