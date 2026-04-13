# Token Optimization Plan

## Current state

128k input tokens per CEO turn. Our actual payload is ~3k tokens.
The other 125k is MCP tool schemas + codex runtime overhead.

Breakdown:
- Our system prompt: ~710 tokens
- Our work packet: ~500 tokens  
- CLAUDE.md + AGENTS.md: ~985 tokens
- Codex runtime/system: ~1,000 tokens
- **27 MCP tool schemas (104 args): ~125,000 tokens** ← the problem

## The problem

Every MCP tool registers a full JSON Schema with name, description, and typed
parameters. 27 tools × ~4,600 tokens/tool = 124,200 tokens. This ships on
EVERY turn, even when the agent will only use 2-3 tools.

## Optimization plan (ordered by impact)

### 1. Per-role tool sets (50-70% reduction, ~2 days)

Most agents only need 3-5 tools. CEO needs broadcast, poll, task, bridge.
Specialists need broadcast, poll, status. Nobody needs all 27 every turn.

**Implementation**: Register different tool sets based on agent role when
creating the MCP server. The server already receives `WUPHF_AGENT_SLUG` env var.

| Role | Tools needed | Current | Savings |
|------|-------------|---------|---------|
| CEO (office) | broadcast, poll, task, bridge, members, status, human_message, human_interview | 27 | 19 tools cut (~70%) |
| CEO (DM) | broadcast, poll, human_message, human_interview | 27 | 23 tools cut (~85%) |
| Specialist | broadcast, poll, status, tasks | 27 | 23 tools cut (~85%) |

Expected result: CEO DM turn drops from 128k to ~20-30k input.

### 2. Shorter tool descriptions (10-15% of remaining)

Current descriptions are verbose. Example:
```
"Create or remove an office channel. When creating a channel, include a clear 
description of what work belongs there and the initial roster that should be 
in it. Only do this when the human explicitly wants channel structure."
```

Can be:
```
"Create or remove a channel."
```

The system prompt already explains when to use each tool. The schema description
doesn't need to repeat it.

### 3. Compact arg schemas (5-10% of remaining)

Many args have verbose `jsonschema` descriptions that repeat the tool description.
Trim to essential type + one-line hint.

### 4. DM-specific MCP server (biggest single win)

For DM mode, register a minimal MCP server with only:
- `team_broadcast` (reply in DM)
- `team_poll` (read conversation)  
- `human_message` (present output)
- `human_interview` (blocking question)

4 tools instead of 27. This alone could drop DM turns from 128k to ~15k input.

### 5. Skip AGENTS.md in headless mode

Codex auto-discovers and loads AGENTS.md (785 tokens). In headless mode where
the system prompt already describes the team, this is redundant. Pass
`--skip-agents-md` or equivalent flag.

### 6. Lazy tool loading (future)

MCP spec supports `tools/list` but not lazy schema loading. Future: implement
a tool discovery pattern where the agent first sees tool names only (~100 tokens),
then calls a meta-tool to load the full schema of a specific tool when needed.

## Expected results

| Scenario | Current | After opt 1+4 | Reduction |
|----------|---------|---------------|-----------|
| CEO DM turn | 128k input | ~20k input | 85% |
| CEO office turn | 128k input | ~45k input | 65% |
| Specialist turn | 128k input | ~20k input | 85% |
| Effective billed (with cache) | ~17k avg | ~5k avg | 70% |

## Impact on benchmark

With optimization 1+4:
- WUPHF avg/turn: 17k → ~5k billed
- Paperclip avg/turn: 44k (unchanged)
- New ratio: **~9x more efficient**

## Priority

1. Per-role tool sets (biggest bang, medium effort)
2. DM-specific MCP server (biggest single-turn win)
3. Shorter descriptions (quick, additive)
4. Skip AGENTS.md (trivial)
5. Compact schemas (additive)
6. Lazy loading (future, spec limitation)
