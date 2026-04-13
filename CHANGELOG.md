# Changelog

All notable changes to WUPHF will be documented in this file.

## [0.0.1.0] - 2026-04-14

### Added
- **Proactive skill suggestions.** CEO agent now detects repeated workflows during normal conversation and proposes reusable skills via `[SKILL PROPOSAL]` blocks. Proposals surface as non-blocking interviews in the Requests panel. One-click accept activates the skill, reject archives it. The system learns from the team's actual work instead of requiring manual prompt editing.
- **Author-gated proposal parsing.** Only the team lead (CEO) can trigger skill proposals via message blocks. Prevents specialists and pasted transcripts from creating false proposals. Empty offices reject all proposals by default.
- **Agent team suggestions via existing tools.** CEO can suggest new specialist agents using the existing `team_member` and `team_channel_member` MCP tools with human approval via `human_interview`. No new data model needed.
- **11 unit tests** covering the full skill proposal lifecycle: CEO happy path, non-CEO rejection, malformed input, dedup, re-proposal after rejection, non-blocking interview creation, accept/reject callbacks, prompt content verification, persistence round-trip.
