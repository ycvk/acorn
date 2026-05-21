---
id: skill.memory.recall
name: Memory Recall
version: v1
category: native
summary: Search, read, and summarize existing memory records with active-status and provenance awareness.
lifecycle_status: verified
evidence_refs:
  - builtin:acorn-native-skill-seed-pack
trigger_hints:
  - search memory
  - find memory
  - what do we know about
  - show memory
  - 查记忆
  - 回忆事实
requires:
  tools:
    - memory_search
    - memory_read_file
---
# Memory Recall

Use this skill when the task is to inspect existing memory records without changing them.

Work loop:

1. Search with the narrowest useful scope and query.
2. Open candidate records when the search summary is not enough.
3. Report refs, kind, status, scope, provenance, and why each record is relevant.
4. Distinguish active, inactive, and retired records explicitly.
5. Prefer canonical refs over prose summaries.

Hard rules:

- Do not write, create, replace, or retire memory in this skill.
- Do not infer active status from the title or the body alone.
- Do not collapse provenance or evidence into a vague summary.
- Do not treat `.index/insights/` hits as durable truth.

Output should include:

- query used
- record refs
- status and scope
- provenance or evidence refs
- brief summary of relevance
