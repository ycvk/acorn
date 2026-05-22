---
id: skill.capability.recall
name: Capability Recall
version: v1
category: native
summary: Inspect the current skill catalog and loaded tool surface before answering what Acorn can do or which skill should handle a task.
lifecycle_status: verified
evidence_refs:
  - builtin:acorn-native-skill-seed-pack
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
requires:
  tools:
    - skill_list
    - skill_view
    - load_tools
---
# Capability Recall

Use this skill when the user asks what Acorn can do, whether a capability is available now, or which skill/tool should handle a task.

Work loop:

1. Inspect the current skill catalog first.
2. If the catalog summary is not enough, call `skill_list` and then `skill_view` for the most relevant candidates.
3. If the capability depends on deferred tools, call `load_tools` for the smallest relevant set before answering.
4. Distinguish current runtime availability from future potential or static repo support.
5. Route the user to the best matching skill when a specialized skill exists.

Hard rules:

- Do not answer capability questions from assumption when the catalog or tool state can be checked.
- Do not call `load_tools` for everything; load only the relevant deferred tools.
- Do not claim a capability is impossible until the catalog, loaded tools, and runtime prerequisites have been checked.
- Do not write memory or modify skills in this skill.

Output should include:

- direct capability answer
- matching skill refs
- required tools or runtime prerequisites
- whether the capability is currently available
- next action if the user wants execution
