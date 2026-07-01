// Shared plumbing for the one-shot pi-ai model calls (buildAgent.ts, tools.ts,
// capabilities.ts): abort-reason normalization, text extraction, and the
// hand-built "caller signal + hard timeout -> one AbortSignal" combinator.

/** Normalize an abort reason into an Error (signals may carry anything). */
export function asError(reason: unknown, fallback = "aborted"): Error {
	if (reason instanceof Error) return reason;
	return new Error(typeof reason === "string" ? reason : fallback);
}

/** Concatenate the text parts of a pi-ai completion's content. */
export function textOf(content: { type: string; text?: string }[]): string {
	return content
		.filter((c) => c.type === "text" && typeof c.text === "string")
		.map((c) => c.text as string)
		.join("");
}

export interface Deadline {
	/** Aborts on the caller's signal OR the hard timeout, whichever fires first. */
	signal: AbortSignal;
	/** Release the timer + listener. Call in `finally`. */
	done(): void;
}

/**
 * One signal that aborts on either the caller's signal or the timeout. Built by
 * hand (not AbortSignal.any) so it works across runtimes.
 *
 * - `timeoutMessage` is the Error message when the deadline fires.
 * - `abortFallback` is the Error message when the caller's signal carries no
 *   usable reason.
 */
export function deadlineSignal(
	signal: AbortSignal | undefined,
	timeoutMs: number,
	opts: { timeoutMessage: string; abortFallback?: string },
): Deadline {
	const ctrl = new AbortController();
	const onAbort = () => ctrl.abort(asError(signal?.reason, opts.abortFallback));
	if (signal?.aborted) ctrl.abort(asError(signal.reason, opts.abortFallback));
	else signal?.addEventListener("abort", onAbort, { once: true });
	const timer = setTimeout(() => ctrl.abort(new Error(opts.timeoutMessage)), timeoutMs);
	return {
		signal: ctrl.signal,
		done() {
			clearTimeout(timer);
			signal?.removeEventListener("abort", onAbort);
		},
	};
}
