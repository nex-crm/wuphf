package team

// office_eval_jobs_interview_dedupe.go — the `interview-dedupe` eval job.
//
// Live smoke run: FIVE agents asked the human the same "which CRM?"
// question in five separate blocking interviews; answering one did not
// ground the rest. This job replays that failure at the HTTP layer with
// the exact wire the MCP human_interview tool fires (POST /requests →
// poll GET /interview/answer):
//
//	(a) two agents raise semantically-similar questions → ONE pending card,
//	    second asker attached as also_asking;
//	(b) answering the one card fans the answer out to BOTH askers (same
//	    request id, both polls carry the answer, both tagged in the
//	    answer message);
//	(c) a third similar ask AFTER the answer gets the existing answer
//	    immediately — no new card;
//	(d) two genuinely DIFFERENT questions still raise two cards;
//	(e) two action-approvals for different actions are NEVER collapsed.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

func evalJobInterviewDedupe(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "interview-dedupe"

	mux := http.NewServeMux()
	mux.HandleFunc("/requests", fx.broker.requireAuth(fx.broker.handleRequests))
	mux.HandleFunc("/requests/answer", fx.broker.requireAuth(fx.broker.handleRequestAnswer))
	mux.HandleFunc("/interview/answer", fx.broker.requireAuth(fx.broker.handleInterviewAnswer))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	postInterview := func(from, question string) (int, interviewCreateResponse, error) {
		// Exact MCP wire: teammcp handleHumanInterview → POST /requests.
		status, raw, err := client.postJSON("/requests", map[string]any{
			"kind": "interview", "channel": "general", "from": from,
			"title": "Human interview", "question": question, "context": "",
			"blocking": false, "required": false, "reply_to": "", "issue_id": "",
		})
		if err != nil {
			return 0, interviewCreateResponse{}, err
		}
		var parsed interviewCreateResponse
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return status, interviewCreateResponse{}, fmt.Errorf("parse %q: %w", raw, err)
		}
		return status, parsed, nil
	}

	pendingInterviewIDs := func() []string {
		var ids []string
		for _, req := range fx.broker.Requests("general", false) {
			if requestIsHumanInterview(req) && requestIsActive(req) {
				ids = append(ids, req.ID)
			}
		}
		return ids
	}

	// ── (a) two agents, same question, different phrasing → ONE card ───────
	const q1 = "Which CRM should the team standardize on for the pilot — HubSpot or Salesforce?"
	const q1Rephrased = "Which CRM do you want us to standardize on: HubSpot or Salesforce?"
	st1, first, err := postInterview("eng", q1)
	if err != nil {
		return err
	}
	st2, second, err := postInterview("ceo", q1Rephrased)
	if err != nil {
		return err
	}
	pending := pendingInterviewIDs()
	r.add(job, "a second agent asking a similar question merges onto the existing card (one pending interview)",
		st1 == http.StatusOK && st2 == http.StatusOK && first.ID != "" &&
			second.Deduped && second.ID == first.ID && len(pending) == 1,
		fmt.Sprintf("first=%s second=%s deduped=%v pending=%d", first.ID, second.ID, second.Deduped, len(pending)), "")
	r.add(job, "the merged asker rides the existing request as an also_asking subscriber",
		len(second.Request.AlsoAsking) == 1 && second.Request.AlsoAsking[0] == "ceo",
		fmt.Sprintf("also_asking=%v", second.Request.AlsoAsking), "")

	// ── (b) one human answer grounds BOTH askers ────────────────────────────
	const theAnswer = "HubSpot — we already pay for it; pilot stays on HubSpot."
	ansStatus, ansBody, err := client.postJSON("/requests/answer", map[string]any{
		"id": first.ID, "choice_id": "answer_directly", "custom_text": theAnswer,
	})
	if err != nil {
		return err
	}
	// Both agents poll the SAME id (the dedupe handed ceo eng's request id),
	// so one GET per asker is exactly the wire each poll loop fires.
	pollOK := true
	for range 2 {
		_, pollBody, err := client.getJSON("/interview/answer?id=" + first.ID)
		if err != nil {
			return err
		}
		var poll struct {
			Answered *struct {
				CustomText string `json:"custom_text"`
			} `json:"answered"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(pollBody), &poll); err != nil {
			return err
		}
		if poll.Status != "answered" || poll.Answered == nil || poll.Answered.CustomText != theAnswer {
			pollOK = false
		}
	}
	var answerMsg *channelMessage
	for _, m := range fx.broker.ChannelMessages("general") {
		if strings.Contains(m.Content, "Answered @eng's request") {
			msg := m
			answerMsg = &msg
		}
	}
	fanout := answerMsg != nil && len(answerMsg.Tagged) == 2 &&
		answerMsg.Tagged[0] == "eng" && answerMsg.Tagged[1] == "ceo"
	r.add(job, "answering the one card delivers the answer to BOTH askers' polls and tags both",
		ansStatus == http.StatusOK && pollOK && fanout,
		fmt.Sprintf("answer=%d body=%s pollOK=%v fanout=%v", ansStatus, truncate(ansBody, 80), pollOK, fanout), "")

	// ── (c) a similar ask AFTER the answer → immediate answer, no card ──────
	st3, third, err := postInterview("eng", "Which CRM should we standardize on — HubSpot or Salesforce?")
	if err != nil {
		return err
	}
	pending = pendingInterviewIDs()
	r.add(job, "a similar ask after the answer returns the existing answer immediately and raises no card",
		st3 == http.StatusOK && third.Deduped && third.AlreadyAnswered &&
			strings.Contains(third.Answer, "HubSpot") && len(pending) == 0,
		fmt.Sprintf("deduped=%v already_answered=%v answer=%q pending=%d",
			third.Deduped, third.AlreadyAnswered, truncate(third.Answer, 60), len(pending)), "")

	// ── (d) two DIFFERENT questions → two cards ─────────────────────────────
	st4, diffA, err := postInterview("eng", "What is the budget ceiling for paid pilot tooling this quarter?")
	if err != nil {
		return err
	}
	st5, diffB, err := postInterview("ceo", "Should we send the renewal email to Acme before their QBR?")
	if err != nil {
		return err
	}
	pending = pendingInterviewIDs()
	r.add(job, "two genuinely different questions still raise two separate cards",
		st4 == http.StatusOK && st5 == http.StatusOK &&
			!diffA.Deduped && !diffB.Deduped && diffA.ID != diffB.ID && len(pending) == 2,
		fmt.Sprintf("a=%s b=%s pending=%d", diffA.ID, diffB.ID, len(pending)), "")

	// ── (e) approvals for different actions are NEVER collapsed ─────────────
	// Same question text, distinct actions (distinct dedupe keys) — the
	// scope guard must keep semantic dedupe away from approval gates.
	postApproval := func(from, dedupeKey string) (int, interviewCreateResponse, error) {
		status, raw, err := client.postJSON("/requests", map[string]any{
			"kind": "approval", "channel": "general", "from": from,
			"title": "Approve the send", "question": "Approve sending the renewal email now?",
			"dedupe_key": dedupeKey,
		})
		if err != nil {
			return 0, interviewCreateResponse{}, err
		}
		var parsed interviewCreateResponse
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return status, interviewCreateResponse{}, fmt.Errorf("parse %q: %w", raw, err)
		}
		return status, parsed, nil
	}
	st6, apprA, err := postApproval("eng", "eng:gmail:send:acme")
	if err != nil {
		return err
	}
	st7, apprB, err := postApproval("ceo", "ceo:gmail:send:corti")
	if err != nil {
		return err
	}
	r.add(job, "two action-approvals for different actions are never collapsed",
		st6 == http.StatusOK && st7 == http.StatusOK &&
			!apprA.Deduped && !apprB.Deduped && apprA.ID != "" && apprA.ID != apprB.ID,
		fmt.Sprintf("a=%s b=%s dedupedA=%v dedupedB=%v", apprA.ID, apprB.ID, apprA.Deduped, apprB.Deduped), "")

	return nil
}

// interviewCreateResponse decodes POST /requests create responses,
// including the semantic-dedupe fields the MCP tool reads.
type interviewCreateResponse struct {
	ID              string `json:"id"`
	Deduped         bool   `json:"deduped"`
	AlreadyAnswered bool   `json:"already_answered"`
	Answer          string `json:"answer"`
	Request         struct {
		ReplyTo    string   `json:"reply_to"`
		AlsoAsking []string `json:"also_asking"`
	} `json:"request"`
}
