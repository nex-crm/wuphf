import { useCallback, useState } from "react";

import { type CompileResult, compileWiki } from "../../api/sources";

/**
 * Compile trigger for the wiki. POSTs /wiki/compile to run the deterministic
 * compile engine over the immutable source layer, then surfaces the returned
 * CompileResult (pages written / concepts / sources read / warnings) inline.
 *
 * Compile is a state-changing action (it writes articles into the wiki) but
 * one the operator explicitly invokes here, so it runs on click without a
 * second confirmation — the result summary makes what happened legible.
 */

type CompileState =
  | { status: "idle" }
  | { status: "running" }
  | { status: "done"; result: CompileResult }
  | { status: "error"; message: string };

interface CompileButtonProps {
  /** Injectable for tests/Storybook; defaults to the real client. */
  compile?: () => Promise<CompileResult>;
  /** Called after a successful compile (e.g. to refetch the catalog). */
  onCompiled?: (result: CompileResult) => void;
}

export default function CompileButton({
  compile = compileWiki,
  onCompiled,
}: CompileButtonProps) {
  const [state, setState] = useState<CompileState>({ status: "idle" });

  const run = useCallback(() => {
    setState({ status: "running" });
    compile()
      .then((result) => {
        setState({ status: "done", result });
        onCompiled?.(result);
      })
      .catch((err: unknown) => {
        setState({
          status: "error",
          message:
            err instanceof Error ? err.message : "Compile failed. Try again.",
        });
      });
  }, [compile, onCompiled]);

  const running = state.status === "running";

  return (
    <div className="wk-compile">
      <button
        type="button"
        className="wk-compile-btn"
        onClick={run}
        disabled={running}
        aria-busy={running}
      >
        {running ? "Compiling…" : "Compile wiki"}
      </button>
      {state.status === "done" ? (
        <CompileSummary result={state.result} />
      ) : null}
      {state.status === "error" ? (
        <span className="wk-compile-error" role="alert">
          {state.message}
        </span>
      ) : null}
    </div>
  );
}

function CompileSummary({ result }: { result: CompileResult }) {
  const warnings = result.errors ?? [];
  return (
    <div className="wk-compile-result" role="status">
      <span className="wk-compile-tally">
        {result.pages_written} page{result.pages_written === 1 ? "" : "s"}{" "}
        written · {result.concepts} concept
        {result.concepts === 1 ? "" : "s"} · {result.sources_read} source
        {result.sources_read === 1 ? "" : "s"} read
      </span>
      {warnings.length > 0 ? (
        <details className="wk-compile-warnings">
          <summary>
            {warnings.length} warning{warnings.length === 1 ? "" : "s"}
          </summary>
          <ul>
            {warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
}
