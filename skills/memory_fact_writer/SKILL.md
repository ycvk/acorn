---
id: skill.memory.fact.writer
name: Memory Fact Writer
version: v1
category: native
summary: Create, replace, or retire durable fact records under `facts/` using the current Record V2 schema.
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
    - memory_read_file
    - memory_create_file
    - memory_replace_span
---
# Memory Fact Writer

Use this skill when the task is to add or update durable factual memory.

Work loop:

1. Search for an existing fact before writing a new one.
2. Choose the narrowest valid scope, usually `user` or `workspace:{slug}`.
3. Draft valid Record V2 frontmatter before prose body content.
4. Write only under `facts/` and only with a `.md` file path.
5. Use the current schema fields: `scope`, `tags`, `status`, `created`, `updated`, `valid_from`, `valid_until`, `source_run`, `source_refs`, `evidence_refs`, and `relations`.
6. Call `memory_create_file` for new records or `memory_replace_span` for existing ones.
7. If the planner rejects the write, read the reason, fix the schema, and try again.

Hard rules:

- Do not use old fields like `source`.
- Do not write run-only noise as a durable fact.
- Do not change the ref unless you are intentionally retiring and recreating the record.
- Do not bypass rejection with fallback prose or a second schema.

Output should include:

- path
- ref
- action taken
- resulting status
- evidence refs used
- whether a rebuild or verification step succeeded
