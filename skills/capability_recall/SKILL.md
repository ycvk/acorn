---
id: skill.capability.recall
name: Capability Recall
version: v1
category: native
summary: Inspect the current skill catalog and loaded tool surface before answering what Acorn can do or which skill should handle a task.
trigger_hints:
  - what can you do
  - can you do this
  - do you support
  - which skill should I use
  - which skill fits
  - capability question
  - ability question
  - what skill
  - use what skill
  - 你会什么
  - 你能做什么
  - 你支持什么
  - 该用哪个 skill
  - 该用什么 skill
  - 能不能联网
  - 会联网吗
  - 联网能力
---
# Capability Recall

Use this skill when the user asks what Acorn can do, whether a capability is available now, or which skill/tool should handle a task.

Work loop:

1. Read the skill list in the `skill` tool's description first.
2. Load the most relevant candidates with the `skill` tool when their descriptions are not enough.
3. If the capability depends on deferred tools, call `tool_search` for the smallest relevant set before answering.
4. Distinguish current runtime availability from future potential or static repo support.
5. Route the user to the best matching skill when a specialized skill exists.

Hard rules:

- Do not answer capability questions from assumption when the catalog or tool state can be checked.
- Do not load every deferred tool through `tool_search`; load only the relevant ones.
- Do not claim a capability is impossible until the catalog, loaded tools, and runtime prerequisites have been checked.
- Do not write memory or modify skills in this skill.

Output should include:

- direct capability answer
- matching skill refs
- required tools or runtime prerequisites
- whether the capability is currently available
- next action if the user wants execution
