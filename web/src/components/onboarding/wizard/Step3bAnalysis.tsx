import { useEffect, useState } from "react";
import { post } from "../../../api/client";
import type { OSScanResponse } from "../../../api/onboarding";

interface AnalysisStepProps {
  website: string;
  ownerName: string;
  ownerRole: string;
  onDone: (result: OSScanResponse) => void;
  onSkip: () => void;
}

type ScanStatus = "scanning" | "done-flash" | "revealing";

export function AnalysisStep({
  website,
  ownerName,
  ownerRole,
  onDone,
  onSkip,
}: AnalysisStepProps) {
  const [status, setStatus] = useState<ScanStatus>("scanning");
  const [result, setResult] = useState<OSScanResponse | null>(null);
  const [showContinue, setShowContinue] = useState(false);

  // biome-ignore lint/correctness/useExhaustiveDependencies: intentionally runs once on mount; props are stable at the time this step is rendered
  useEffect(() => {
    let cancelled = false;
    async function runScan() {
      try {
        const data = await post<OSScanResponse>("/onboarding/scan", {
          website_url: website,
          file_paths: [],
          owner_name: ownerName,
          owner_role: ownerRole,
        });
        if (cancelled) return;
        setResult(data);
        setStatus("done-flash");
        setTimeout(() => {
          if (!cancelled) setStatus("revealing");
        }, 200);
      } catch {
        if (!cancelled) onSkip();
      }
    }
    void runScan();
    return () => {
      cancelled = true;
    };
  }, []); // only run once on mount

  const items = result
    ? [...(result.articles_written ?? []), ...(result.facts ?? [])]
    : [];

  useEffect(() => {
    if (status !== "revealing") return;
    const t = setTimeout(() => setShowContinue(true), items.length * 120 + 350);
    return () => clearTimeout(t);
  }, [status, items.length]);

  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        {status === "scanning" && (
          <div className="analysis-scanning">
            <span className="analysis-spinner" aria-label="Scanning" />
            <p className="analysis-status-text">Analyzing your company...</p>
          </div>
        )}
        {status === "done-flash" && (
          <p className="analysis-status-text analysis-done-flash">Done.</p>
        )}
        {status === "revealing" && result && (
          <div className="analysis-reveal">
            {result.articles_written?.map((article, i) => (
              <div
                key={`article-${i}`}
                className="reveal-item"
                style={{ ["--i" as string]: i } as React.CSSProperties}
              >
                <span className="reveal-check">✓</span>
                <span className="reveal-article">{article}</span>
                <span className="unread-dot" aria-hidden="true" />
              </div>
            ))}
            {result.facts?.map((fact, i) => (
              <div
                key={`fact-${i}`}
                className="reveal-item"
                style={
                  {
                    ["--i" as string]:
                      (result.articles_written?.length ?? 0) + i,
                  } as React.CSSProperties
                }
              >
                <span className="reveal-fact">{fact}</span>
              </div>
            ))}
            {showContinue && (
              <p className="analysis-caption">Your agents already know this.</p>
            )}
          </div>
        )}
      </div>
      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onSkip} type="button">
          Skip
        </button>
        {showContinue && (
          <button
            className="btn btn-primary"
            onClick={() => result && onDone(result)}
            type="button"
          >
            Continue
          </button>
        )}
      </div>
    </div>
  );
}
