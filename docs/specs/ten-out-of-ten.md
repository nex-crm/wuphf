# Ten out of Ten — the complete gap inventory

Every gap standing between the v3 grade (6/10) and 10/10, compiled from all three grading reports (v1 5/10, v2 5.5/10, v3 6/10 — `.icp-eval/`). Doctrine: fix ALL of it, each fix with a live-path regression test (eval check at the layer the bug lived), THEN re-run the eval once. No more half-point iterations.

Already covered by round-2 (fix-1 `live-paths`, fix-2 `utterance-routing`, fix-3 `workspace-isolation` — landed/in flight) and excluded here: state machine + dead verbs + zero-work approvals + dependency release + board/page split; request-changes text + thread replies + waiting-state wakes + interview wedge + loud interviews; agent cwd isolation + destructive-op discipline + the J3 DoD chain.

## Wave A — task-model integrity (broker mutation/notify)
| ID | Gap (evidence) | Fix direction |
|----|----|----|
| A1 | Self-heal lanes duplicate/steal primary work; deliverables ship from OFFICE-295 while primaries sit empty (V3-N8) | Self-heal tasks are SUB-tasks of the stalled parent; before creating, dedupe vs existing open lanes for the same work; when a self-heal lane delivers, the artifact + done attach to the PARENT (primary) task |
| A2 | Pack lanes self-started despite "queued… whenever you want"; task simultaneously "delivered" and "waiting on you to start" (V3-N9) | Pack/auto-created lanes obey the same drafting→human-activation gate as composer tasks; states must be mutually exclusive — derive the chip and the hint from the one lifecycle state |
| A3 | Title truncation STILL fed one agent pass → second conflicting Corti brief (v3 [17:41:35]) | Find the remaining truncated-title→context feed (subtask creation copying clipped parent title into details, or CEO echo) and kill it: agent-facing content always from Details/Definition |
| A4 | 6× repeated done-messages; triple identical commits (v3 [18:05–18:10], [20:15]) | Idempotency: done-post per (task, terminal transition) exactly once; wiki commit dedupe for identical content writes |
| A5 | Approve card named the wrong agent (v3 [18:44:21]) | Decision card actor = the task owner/submitting agent, from the task record not the packet's last actor |

## Wave B — knowledge layer (wiki worker / entity / git)
| ID | Gap | Fix direction |
|----|----|----|
| B1 | Entity graph EMPTY of customers after 2 full journeys, 3 runs in a row: 4 nodes 0 edges, People-only (V2-N7, v3 [20:14]) | Trace why B1-extraction never fired live (distill gated on verified-done? terminal paths skipping queueTaskDistillation? extraction finding no entities in real Definitions?) — bind extraction to EVERY terminal-done path; ensure company-kind extraction from goal/deliverables; ensure RecordFactRefs writes edges; live-path eval: HTTP-driven task done → graph has the company nodes + co-occurrence edge |
| B2 | Duplicates compound into the knowledge base: two Corti briefs on disk, "Playbook: X" + "X", 4 review paths double-submitted (v3 [17:39:51], [18:19:45], [20:15]) | Update-first enforcement at the wiki WRITE boundary: slug-similarity check (reuse Jaro-Winkler tier) on agent article writes → route to update/append instead of new file; review submission dedupe per path+content-hash |
| B3 | Git history attributes agent writes to "human · wiki: external edit" — 3 runs (v1#8, v3 [20:15], [17:41:21]) | Find the bulk/external-edit commit path and attribute the acting agent (author identity exists — Librarian commits correctly); Recent-changes rows show the real author |
| B4 | "173 revisions" on a minutes-old article; commit storms | Coalesce identical/rapid successive writes per path (the worker queue can fold) |
| B5 | Deliverables shipped chat-only, never wiki'd (QBR one-pager = msg-303 only; V3-N10) | The artifact gate already blocks defined tasks; close the leak: when a deliverable-shaped completion happens on ANY lane (incl. self-heal → parent attach from A1), the done path requires the wiki artifact; CEO prompt line: deliverables land in the wiki first, chat carries the link |

## Wave C — surface honesty (FE + stats)
| ID | Gap | Fix direction |
|----|----|----|
| C1 | Count drift everywhere, 3 runs: header blocked=1 vs board 0; Inbox 10 vs 11; wiki "0 articles" vs 19; "Needs human input 0" with 2 live; "6 active" vs all-waiting (v1#7) | One derived-stats source: a single broker endpoint serving the counts every surface consumes (header, board, dashboard, inbox badge, wiki home); eval check compares list endpoints vs the stats endpoint |
| C2 | Usage pill $0.0000 for 75 min while popover knows $45.74/$18.66 (v1#9) | Pill reads the same aggregate the popover reads, updated on turn settle (SSE push or refetch-on-usage-event) |
| C3 | Notebooks tab never loads (60s+ spinner, v3 [18:37:55]) | Find the hang (query without timeout? broker route missing?) — fix + honest error state |
| C4 | Wedge-time lying: board rendered EMPTY, Reviews rendered zeros over a hung load (V3-N7 render half) | Honest loading discipline: surfaces never render empty/zero as fact while the query is pending/errored — skeleton + "broker not responding" states |
| C5 | Live needs-action card said "answered or no longer active" (fix-2 scoped requests to all — verify residue) | Verify + pin with a component test |

## Wave D — scheduler truth
| ID | Gap | Fix direction |
|----|----|----|
| D1 | Standing automations are ghosts, 3 runs: agent-registered crons session-scoped, invisible in Scheduled Tasks, 7-day death, TWO crons for one ask (v1#11, v3 [18:30–18:36]) | Agent cron registration goes through the persistent broker scheduler (visible+editable in Scheduled Tasks); dedupe by normalized purpose/schedule; kill or mirror the session-scoped path; live-path eval: MCP cron-create → GET scheduler shows it |

## Wave E — language at the human boundary (prompts + copy)
| ID | Gap | Fix direction |
|----|----|----|
| E1 | Jargon/tool leaks at Maya, 3 runs: `blocked_on_pr_merge`, "call team_task action=submit_for_review", raw tool_reference JSON in typing strip, `signal: killed`, "broker requires a direct human message ID", sarcastic "Of course it didn't. Classic." (v1#12) | Render-boundary humanization: FE filters for tool/JSON/enum leak patterns in chat + activity + typing strip; replace enum statuses with plain labels; delete sarcastic filler copy; prompt line: never name internal tools/enums in human-facing messages |
| E2 | Fact mutated by paraphrase ("usage up 40%" → "at 40% usage") | Prompt: numbers and quoted facts reproduce VERBATIM from source |
| E3 | Interview asked for facts the human already gave (email, sender name) | Prompt: before interviewing, re-read the thread + definition for the answer; interview only what is absent |
| E4 | Skill attributed to "@archivist" — not a roster name (v3 [18:21:15]) | Attribute compiled artifacts to the real acting identity (librarian) consistently |
| E5 | No define-time interview for genuine gaps — CEO wrote around [CONTACT NAME] holes (v3 step-2/3 deduction) | Strengthen the intake contract: placeholders/access-needed in a Definition REQUIRE the batched interview before staffing; eval: definition with a placeholder → interview raised before subtasks |

## Wave F — platform under load (after A, careful scope)
| ID | Gap | Fix direction |
|----|----|----|
| F1 | Broker wedges freeze wiki/board/creation 4.5 min (V3-N7) | Targeted lock-hold reduction on the endpoints that wedged (wiki reads via snapshot, list endpoints copy-under-lock-then-serialize), plus request timeouts with structured errors (FE shows C4's honest states); NOT a full lock redesign |
| F2 | 21-min running-but-silent stall; `signal: killed` exhaust as the only trace (v3 [19:05:30]) | The activity watchdog must mark silent-running tasks visibly stalled (chip + chat note) after N minutes; killed turns post an honest one-line system note |

## Execution rules
- Every fix ships a regression test at the live layer (house rule; eval check preferred — `live-paths` discipline).
- Waves A–D dispatch in parallel (file-disjoint); E and F after fix-3 lands (prompt/runner overlap).
- Full suites + office-eval green after every wave; one commit per wave.
- THEN: v4 live re-run, once.
