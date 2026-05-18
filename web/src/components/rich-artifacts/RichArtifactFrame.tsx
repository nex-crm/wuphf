import { useEffect, useMemo, useRef, useState } from "react";

import "../../styles/rich-artifacts.css";

const RESIZE_EVENT_TYPE = "wuphf:rich-artifact:resize";

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

function withSandboxCsp(html: string, frameId?: string): string {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${SANDBOX_CSP}">`;
  const resizeScript = frameId ? buildResizeScript(frameId) : "";
  if (/<html[\s>]/i.test(html)) {
    return injectIntoDocument(html, meta, resizeScript);
  }
  return `<!doctype html><html><head>${meta}</head><body>${html}${resizeScript}</body></html>`;
}

function injectIntoDocument(
  html: string,
  headContent: string,
  bodyTail: string,
) {
  let documentHtml = html;
  if (/<head[\s>]/i.test(documentHtml)) {
    documentHtml = documentHtml.replace(
      /<head([^>]*)>/i,
      `<head$1>${headContent}`,
    );
  } else {
    documentHtml = documentHtml.replace(
      /<html([^>]*)>/i,
      `<html$1><head>${headContent}</head>`,
    );
  }
  if (/<\/body>/i.test(documentHtml)) {
    return documentHtml.replace(/<\/body>/i, `${bodyTail}</body>`);
  }
  return `${documentHtml}${bodyTail}`;
}

function buildResizeScript(frameId: string): string {
  const encodedFrameId = JSON.stringify(frameId).replace(/</g, "\\u003c");
  const encodedEventType = JSON.stringify(RESIZE_EVENT_TYPE);
  return `<script>(()=>{const id=${encodedFrameId};const type=${encodedEventType};let raf=0;function measure(){const body=document.body;const html=document.documentElement;if(!body||!html)return;const height=Math.ceil(Math.max(body.scrollHeight,body.offsetHeight,html.clientHeight,html.scrollHeight,html.offsetHeight));parent.postMessage({type,id,height},"*")}function schedule(){if(raf)cancelAnimationFrame(raf);raf=requestAnimationFrame(measure)}window.addEventListener("load",schedule);window.addEventListener("resize",schedule);new MutationObserver(schedule).observe(document.documentElement,{attributes:true,characterData:true,childList:true,subtree:true});if("ResizeObserver"in window){const observer=new ResizeObserver(schedule);observer.observe(document.documentElement);if(document.body)observer.observe(document.body)}schedule();setTimeout(schedule,250);setTimeout(schedule,1000)})();</script>`;
}

function frameKey(title: string, html: string): string {
  let hash = 5381;
  const input = `${title}\u0000${html}`;
  for (let i = 0; i < input.length; i += 1) {
    hash = (hash * 33) ^ input.charCodeAt(i);
  }
  return `rich-artifact-${hash >>> 0}`;
}

type RichArtifactFrameVariant = "inline" | "modal";

interface RichArtifactFrameProps {
  title: string;
  html: string;
  variant?: RichArtifactFrameVariant;
  minHeight?: number;
  maxHeight?: number;
  autoResize?: boolean;
}

export default function RichArtifactFrame({
  title,
  html,
  variant = "inline",
  minHeight = 220,
  maxHeight = 12000,
  autoResize,
}: RichArtifactFrameProps) {
  const frameRef = useRef<HTMLIFrameElement | null>(null);
  const frameId = useMemo(() => frameKey(title, html), [title, html]);
  const shouldAutoResize = autoResize ?? variant === "inline";
  const srcDoc = useMemo(
    () => withSandboxCsp(html, shouldAutoResize ? frameId : undefined),
    [frameId, html, shouldAutoResize],
  );
  const [heightState, setHeightState] = useState<{
    frameId: string;
    height: number;
  } | null>(null);
  const height = heightState?.frameId === frameId ? heightState.height : null;

  useEffect(() => {
    if (!shouldAutoResize) return undefined;
    function handleMessage(event: MessageEvent) {
      if (event.source !== frameRef.current?.contentWindow) return;
      if (!isResizeMessage(event.data) || event.data.id !== frameId) return;
      const nextHeight = Math.min(
        Math.max(Math.ceil(event.data.height), minHeight),
        maxHeight,
      );
      setHeightState({ frameId, height: nextHeight });
    }
    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, [frameId, maxHeight, minHeight, shouldAutoResize]);

  return (
    <iframe
      ref={frameRef}
      key={frameId}
      className={`rich-artifact-frame rich-artifact-frame-${variant}`}
      data-testid="rich-artifact-frame"
      title={title}
      sandbox="allow-scripts"
      scrolling={shouldAutoResize ? "no" : undefined}
      srcDoc={srcDoc}
      style={height ? { height } : undefined}
    />
  );
}

function isResizeMessage(
  value: unknown,
): value is { type: typeof RESIZE_EVENT_TYPE; id: string; height: number } {
  if (!value || typeof value !== "object") return false;
  const message = value as Record<string, unknown>;
  return (
    message.type === RESIZE_EVENT_TYPE &&
    typeof message.id === "string" &&
    typeof message.height === "number" &&
    Number.isFinite(message.height)
  );
}
