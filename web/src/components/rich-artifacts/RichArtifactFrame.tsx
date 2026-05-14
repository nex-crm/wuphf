import "../../styles/rich-artifacts.css";

const SANDBOX_CSP = [
  "default-src 'none'",
  "style-src 'unsafe-inline'",
  "script-src 'unsafe-inline'",
  "img-src data: blob:",
  "font-src data:",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
].join("; ");

function withSandboxCsp(html: string): string {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${SANDBOX_CSP}">`;
  if (/<head(\s[^>]*)?>/i.test(html)) {
    return html.replace(/<head(\s[^>]*)?>/i, (match) => `${match}${meta}`);
  }
  return `<!doctype html><html><head>${meta}</head><body>${html}</body></html>`;
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
      className="rich-artifact-frame"
      title={title}
      sandbox="allow-scripts"
      srcDoc={withSandboxCsp(html)}
    />
  );
}
