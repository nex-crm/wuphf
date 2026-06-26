package team

// broker_compile.go owns the manual trigger for the compile engine (S3). A
// POST to /wiki/compile builds a Compiler over the live wiki worker + repo and
// runs a full recompile, returning the CompileResult as JSON.
//
//	POST /wiki/compile -> {pages_written, concepts, sources_read, errors?}
//
// The compile is synchronous: the LLM calls are bounded-concurrency inside
// Compile, and the handler blocks until the run finishes so the caller sees a
// real tally. This is a developer/operator trigger; automatic compile-on-
// capture scheduling lands in a later slice.

import "net/http"

// handleWikiCompile runs a full recompile of the source layer into team/
// articles and returns the tally.
func (b *Broker) handleWikiCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki compile")
	if worker == nil {
		return
	}
	compiler := NewCompiler(worker.Repo(), worker, nil)
	result, err := compiler.Compile(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
