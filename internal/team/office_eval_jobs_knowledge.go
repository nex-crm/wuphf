package team

// office_eval_jobs_knowledge.go — the `knowledge-integrity` eval job
// (ten-out-of-ten Wave B). Every check replicates a v3 live failure at the
// layer the bug lived:
//
//	(a) B1 — the entity graph stayed EMPTY after two full journeys
//	    ([20:14] "4 nodes · 0 edges", People-only) because every done task
//	    terminalized through the DECISION path (/tasks/{id}/decision
//	    approve → LifecycleStateApproved), which never queued the
//	    distillation hook. This check drives the exact FE wire: HTTP
//	    create → define → activate → complete → human decision-approve,
//	    then requires both named companies as graph nodes plus their
//	    co-occurrence edge from the graph endpoint.
//	(b) B2 — agent creates with a similar slug duplicated articles on disk
//	    (two Corti briefs, [20:15]); the write boundary must route the
//	    create onto the existing article instead.
//	(c) B2 — double review submissions of the same (path, content)
//	    stacked duplicate reviews ([17:39:51] 8 reviews = 4 files × 2);
//	    the second submission must collapse onto the open promotion.
//	(d) B3 — git history attributed agent writes to "human · wiki:
//	    external edit"; an agent write through the worker must keep the
//	    agent as the git author.
//	(e) B4 — "173 revisions on a minutes-old article"; a byte-identical
//	    double-write must fold into ONE commit.
//	(f) B5 — chat-only deliverables leaked through phantom artifact paths
//	    (V3-N10); a done whose artifact path does not exist is rejected.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func evalJobKnowledgeIntegrity(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "knowledge-integrity"

	// Wire the entity fact log + graph the way production does
	// (ensureEntitySynthesizer), minus the LLM synthesizer.
	fx.broker.mu.Lock()
	worker := fx.broker.wikiWorker
	fx.broker.factLog = NewFactLog(worker)
	fx.broker.entityGraph = NewEntityGraph(worker)
	fx.broker.mu.Unlock()
	repo := worker.Repo()

	// Serve the exact routes the FE uses, behind the same auth middleware.
	fx.broker.SetReviewerResolver(func(string) string { return "ceo" })
	fx.broker.ensureReviewLog()
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", fx.broker.requireAuth(fx.broker.handleTasks))
	mux.HandleFunc("/tasks/", fx.broker.requireAuth(fx.broker.handleTaskByID))
	mux.HandleFunc("/notebook/write", fx.broker.requireAuth(fx.broker.handleNotebookWrite))
	mux.HandleFunc("/notebook/promote", fx.broker.requireAuth(fx.broker.handleNotebookPromote))
	mux.HandleFunc("/entity/graph/all", fx.broker.requireAuth(fx.broker.handleEntityGraphAll))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	// ── (a) B1: decision-path done populates the entity graph ─────────────
	createStatus, createBody, err := client.postJSON("/tasks", map[string]any{
		"action": "create", "channel": "general",
		"title":   "Close the Q3 renewals",
		"details": "Coordinate the renewal motions for both accounts.",
		"owner":   "eng", "created_by": "ceo",
	})
	if err != nil {
		return err
	}
	var createParsed struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(createBody), &createParsed); err != nil || createStatus != http.StatusOK {
		return fmt.Errorf("create: status=%d body=%s err=%w", createStatus, createBody, err)
	}
	taskID := createParsed.Task.ID
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: taskID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Secure the Q3 renewals for Acme Corp and Brightline Labs",
			Deliverables:    []TaskDeliverable{{Name: "renewals brief", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"Renewals brief published to the wiki"},
		},
	}); err != nil {
		return err
	}
	// No activation step: creation is the authorization — the owner-set
	// create above already landed the task running.
	const renewalsArtifact = "team/accounts/q3-renewals-brief.md"
	if err := fx.seedWikiFile(renewalsArtifact, "# Q3 renewals brief\n"); err != nil {
		return err
	}
	// The agent hands its work to review explicitly — submit_for_review is
	// the wire shape that parked all six v3 tasks in front of the human's
	// Inbox "Approve" button (the decision path under test).
	if status, body, err := client.postJSON("/tasks", map[string]any{
		"action": "submit_for_review", "id": taskID, "channel": "general",
		"created_by": "eng", "artifact_path": renewalsArtifact,
		"details": "Renewals brief ready for review.",
	}); err != nil || status != http.StatusOK {
		return fmt.Errorf("submit_for_review: status=%d body=%s err=%w", status, body, err)
	}
	inReview := fx.broker.TaskByID(taskID)
	r.add(job, "agent submit_for_review parks the task in review (decision-path precondition)",
		inReview != nil && strings.EqualFold(strings.TrimSpace(inReview.status), "review"),
		fmt.Sprintf("status=%q lifecycle=%s", strings.TrimSpace(inReview.status), inReview.LifecycleState), "")
	// Terminalize through the DECISION endpoint — the exact path the v3 run
	// used for all six done tasks, where distillation never fired.
	if status, body, err := client.postJSON("/tasks/"+taskID+"/decision", map[string]any{
		"action": "approve", "created_by": "human",
	}); err != nil || status != http.StatusOK {
		return fmt.Errorf("terminal approve: status=%d body=%s err=%w", status, body, err)
	}
	done := fx.broker.TaskByID(taskID)
	r.add(job, "decision-path approve lands done", done != nil && strings.EqualFold(strings.TrimSpace(done.status), "done"),
		fmt.Sprintf("status=%q lifecycle=%s", strings.TrimSpace(done.status), done.LifecycleState), "")

	// The distillation hook runs as a queued goroutine; poll the graph
	// endpoint (the surface Maya looked at) for both company nodes and
	// their co-occurrence edge.
	type graphResp struct {
		Nodes []GraphAllNode  `json:"nodes"`
		Edges []CoalescedEdge `json:"edges"`
	}
	hasNode := func(g graphResp, kind EntityKind, slug string) bool {
		for _, n := range g.Nodes {
			if n.Kind == kind && n.Slug == slug {
				return true
			}
		}
		return false
	}
	hasEdge := func(g graphResp, a, b string) bool {
		for _, e := range g.Edges {
			if e.FromKind == EntityKindCompanies && e.ToKind == EntityKindCompanies {
				if (e.FromSlug == a && e.ToSlug == b) || (e.FromSlug == b && e.ToSlug == a) {
					return true
				}
			}
		}
		return false
	}
	var graph graphResp
	graphOK := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, graphBody, gerr := client.getJSON("/entity/graph/all")
		if gerr == nil && json.Unmarshal([]byte(graphBody), &graph) == nil &&
			hasNode(graph, EntityKindCompanies, "acme-corp") &&
			hasNode(graph, EntityKindCompanies, "brightline-labs") &&
			hasEdge(graph, "acme-corp", "brightline-labs") {
			graphOK = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.add(job, "graph endpoint returns both company nodes and the co-occurrence edge after a decision-path done",
		graphOK, fmt.Sprintf("nodes=%d edges=%d", len(graph.Nodes), len(graph.Edges)), "")

	// ── (b) B2: similar-slug agent create updates the existing article ────
	const existingRel = "team/accounts/acme-corp-brief.md"
	if _, _, err := worker.Enqueue(context.Background(), "eng", existingRel,
		"# Acme Corp — Account Brief\n\nSeed body.\n", "create", "agent: acme brief"); err != nil {
		return err
	}
	const duplicateRel = "team/accounts/acme-corp-briefing.md"
	if _, _, err := worker.Enqueue(context.Background(), "eng", duplicateRel,
		"## Renewal addendum\n\nUPDATE-FIRST-MARKER\n", "create", "agent: acme briefing"); err != nil {
		return err
	}
	worker.WaitForIdle()
	_, dupErr := os.Stat(filepath.Join(repo.Root(), filepath.FromSlash(duplicateRel)))
	existingBody, _ := os.ReadFile(filepath.Join(repo.Root(), filepath.FromSlash(existingRel)))
	r.add(job, "similar-slug agent create folds into the existing article (no second file)",
		os.IsNotExist(dupErr) && strings.Contains(string(existingBody), "UPDATE-FIRST-MARKER"),
		fmt.Sprintf("duplicateExists=%v existing=%dB", dupErr == nil, len(existingBody)), "")

	// ── (c) B2: duplicate review submission collapses ──────────────────────
	if status, body, err := client.postJSON("/notebook/write", map[string]any{
		"slug": "eng", "path": "agents/eng/notebook/corti-brief.md",
		"content": "# Corti Labs brief\n\nDraft for review.\n", "mode": "create", "commit_message": "seed",
	}); err != nil || status != http.StatusOK {
		return fmt.Errorf("notebook write: status=%d body=%s err=%w", status, body, err)
	}
	promote := func() (string, bool, error) {
		status, body, err := client.postJSON("/notebook/promote", map[string]any{
			"my_slug": "eng", "source_path": "agents/eng/notebook/corti-brief.md",
			"target_wiki_path": "team/accounts/corti-labs.md", "rationale": "Ready for review.",
		})
		if err != nil || status != http.StatusOK {
			return "", false, fmt.Errorf("promote: status=%d body=%s err=%w", status, body, err)
		}
		var parsed struct {
			PromotionID  string `json:"promotion_id"`
			Deduplicated bool   `json:"deduplicated"`
		}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			return "", false, err
		}
		return parsed.PromotionID, parsed.Deduplicated, nil
	}
	firstID, _, err := promote()
	if err != nil {
		return err
	}
	secondID, deduped, err := promote()
	if err != nil {
		return err
	}
	openForTarget := 0
	if rl := fx.broker.ReviewLog(); rl != nil {
		for _, p := range rl.List("all") {
			if p != nil && p.TargetPath == "team/accounts/corti-labs.md" {
				openForTarget++
			}
		}
	}
	r.add(job, "duplicate review submission collapses onto the open promotion",
		firstID != "" && secondID == firstID && deduped && openForTarget == 1,
		fmt.Sprintf("first=%s second=%s deduped=%v openForTarget=%d", firstID, secondID, deduped, openForTarget), "")

	// ── (d) B3: agent wiki write attributed to the agent in git log ───────
	refs, err := repo.Log(context.Background(), existingRel)
	if err != nil {
		return err
	}
	attributed := len(refs) > 0
	for _, ref := range refs {
		if ref.Author != "eng" {
			attributed = false
		}
	}
	authors := make([]string, 0, len(refs))
	for _, ref := range refs {
		authors = append(authors, ref.Author)
	}
	r.add(job, "agent wiki write is attributed to the agent in git history",
		attributed, fmt.Sprintf("authors=%v", authors), "")

	// ── (e) B4: byte-identical double-write produces one commit ───────────
	const foldRel = "team/accounts/fold-probe.md"
	const foldBody = "# Fold probe\n\nIdentical body.\n"
	sha1, _, err := worker.Enqueue(context.Background(), "eng", foldRel, foldBody, "replace", "agent: fold probe")
	if err != nil {
		return err
	}
	sha2, _, err := worker.Enqueue(context.Background(), "eng", foldRel, foldBody, "replace", "agent: fold probe again")
	if err != nil {
		return err
	}
	worker.WaitForIdle()
	foldRefs, err := repo.Log(context.Background(), foldRel)
	if err != nil {
		return err
	}
	r.add(job, "identical double-write folds into one commit",
		sha1 == sha2 && len(foldRefs) == 1,
		fmt.Sprintf("sha1=%s sha2=%s commits=%d", sha1, sha2, len(foldRefs)), "")

	// ── (f) B5: done with a nonexistent artifact path is rejected ─────────
	fID, err := func() (string, error) {
		status, body, err := client.postJSON("/tasks", map[string]any{
			"action": "create", "channel": "general", "title": "Prepare the QBR one-pager",
			"details": "Assemble the QBR one-pager.", "owner": "eng", "created_by": "ceo",
		})
		if err != nil || status != http.StatusOK {
			return "", fmt.Errorf("create f: status=%d body=%s err=%w", status, body, err)
		}
		var parsed struct {
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			return "", err
		}
		return parsed.Task.ID, nil
	}()
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: fID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Ship the QBR one-pager to the wiki",
			Deliverables:    []TaskDeliverable{{Name: "one-pager", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"One-pager published to the wiki"},
		},
	}); err != nil {
		return err
	}
	// Drive the task at done with a PHANTOM artifact path. complete may
	// route through review first depending on the review template, so
	// whichever action would actually land done must carry the rejection.
	const phantomArtifact = "team/accounts/qbr-one-pager-phantom.md"
	phantomStatus, phantomBody, err := client.postJSON("/tasks", map[string]any{
		"action": "complete", "id": fID, "channel": "general",
		"created_by": "eng", "artifact_path": phantomArtifact,
	})
	if err != nil {
		return err
	}
	if mid := fx.broker.TaskByID(fID); phantomStatus == http.StatusOK &&
		mid != nil && !strings.EqualFold(strings.TrimSpace(mid.status), "done") {
		phantomStatus, phantomBody, err = client.postJSON("/tasks", map[string]any{
			"action": "approve", "id": fID, "channel": "general",
			"created_by": "human", "artifact_path": phantomArtifact,
		})
		if err != nil {
			return err
		}
	}
	fAfter := fx.broker.TaskByID(fID)
	r.add(job, "done with a nonexistent artifact path is rejected",
		phantomStatus == http.StatusConflict && strings.Contains(phantomBody, "does not exist") &&
			fAfter != nil && !strings.EqualFold(strings.TrimSpace(fAfter.status), "done"),
		fmt.Sprintf("status=%d body=%s after=%q", phantomStatus, truncate(phantomBody, 160), strings.TrimSpace(fAfter.status)), "")
	return nil
}
