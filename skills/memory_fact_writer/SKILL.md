---
id: skill.memory.fact.writer
name: Memory Fact Writer
version: v1
category: native
summary: Store durable facts under `facts/` with the structured `remember` tool; Acorn generates the Record V2 frontmatter.
lifecycle_status: verified
evidence_refs:
  - builtin:acorn-native-skill-seed-pack
trigger_hints:
  - remember fact
  - store this fact
  - write memory
  - update memory fact
  - 记录事实
  - 保存上下文
requires:
  tools:
    - memory_search
    - remember
    - memory_read_file
    - memory_create_file
    - memory_replace_span
---
# Memory Fact Writer

Use this skill when the task is to store durable factual memory.

Work loop:

1. Search with `memory_search` for an existing fact before storing a new one.
2. To store a new fact, call `remember` with a `title`, the `text`, and optional
   `tags` (and an optional `scope` of `user` or `workspace:{slug}`). Acorn generates
   the record frontmatter and stamps status/created/updated/scope — do not hand-write
   YAML or dates.
3. To edit or retire an existing fact, use `memory_replace_span` (precise patch) or
   `memory_create_file` (full record); these advanced paths take complete frontmatter.

Hard rules:

- Prefer `remember` for new facts; reach for the raw `memory_*` tools only for
  precise edits, retirement, or non-fact files.
- Do not write run-only noise as a durable fact.
- Do not change a record's ref unless you are intentionally retiring and recreating it.

Output should include:

- the stored fact's ref and path
- the action taken
- whether anything was skipped (for example, an equivalent fact already existed)
