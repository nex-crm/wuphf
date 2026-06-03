---
title: Skills and runtimes
created: 2026-06-02
tags: [getting-started, skills, runtimes]
order: 5
---

# Skills and runtimes

Your agents can learn new tricks. The two things that decide what an agent can actually do are its skills and its runtime.

## What a skill is

A skill is a packaged capability an agent can use to do real work. Searching the web, writing a file, querying the wiki, scanning a repository: each of these is a skill. An agent without skills can only talk. An agent with skills can act.

Skills are scoped per agent on purpose. The engineer has the skills an engineer needs, and not the ones it does not, so capability is never broader than the job. When an agent needs a capability it does not have, it can request that the skill be enabled, and you stay in control of saying yes.

This is also how the office grows new abilities over time. A skill written once can be reused by any agent that should have it, so the team gets more capable without you rebuilding anything.

## What a runtime is

A runtime is the engine an agent thinks with. It is the model and provider doing the actual reasoning behind the persona. Claude Code is a runtime. Other providers are runtimes too.

Each agent runs on a runtime, and you can mix them. A simpler agent might run on a lighter, cheaper runtime, while your hardest problems get pointed at your strongest one. You decide where the horsepower goes.

## Why two runtimes beats one

If you only ever connect a single runtime and it hits a rate limit or goes down, the whole office stalls. Adding a second runtime is cheap insurance: the office keeps moving by falling back to the other engine instead of grinding to a halt. It is the difference between a team that takes a coffee break and a team that goes home for the day.
