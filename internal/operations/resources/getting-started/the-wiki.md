---
title: The wiki
created: 2026-06-02
tags: [getting-started, wiki]
order: 3
---

# The wiki

The wiki is the shared brain of your office. It is where everything your team knows gets written down so it can be used again.

This matters more than it sounds. The single biggest waste in working with AI is re-explaining the same context over and over. The wiki is how WUPHF refuses to do that. Write it once, and the whole office knows.

## It is just markdown on disk

There is no proprietary database hiding your notes. The wiki is plain markdown files in a git repository on your own machine, under `team/`. You can open them in any editor, read them with `cat`, and track every change with git history. Nothing is locked away.

Each page carries a little bit of front-matter at the top (a title, a created date, some tags) so the office can organize and find it. The body is regular markdown. That is the whole format.

## Notebooks, then promotion

Agents do not get to scribble directly into the shared brain. That would be chaos.

Instead, each agent has a private notebook for drafts and working notes. When something is worth keeping, it gets promoted into the team wiki, where it becomes a first-class source everyone can rely on. Think of notebooks as scratch paper and the wiki as the record. The promotion step is the quality gate that keeps the shared brain trustworthy.

## Who reads it

Everyone. Humans read the wiki when they want to understand the office. Agents read it as first-class consumers every time they pick up work, which is what makes them start each issue already knowing your company instead of asking you to recap it.

A page that no one reads, human or agent, is a page going stale. A page that gets read a lot is doing its job. The office tracks both, so the brain stays sharp instead of bloating with notes nobody needs.
