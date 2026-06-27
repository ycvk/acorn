---
id: skill.memory.repair
name: Memory Repair
version: v1
category: native
summary: Repair invalid or legacy memory records after rejection, schema drift, or narrow content errors.
trigger_hints:
  - repair memory
  - fix memory file
  - memory schema error
  - retire memory
  - 修复记忆
  - 修正 frontmatter
requires:
  tools:
    - memory_search
    - memory_read_file
    - memory_create_file
    - memory_replace_span
---
# Memory Repair

Use this skill when a memory write failed or when a stored record needs a targeted repair.

Work loop:

1. Read the rejection reason or audit result first.
2. Inspect the target record and preserve its meaning and ref when possible.
3. Fix only the broken fields, relations, dates, or scope.
4. If the record is obsolete but still valid in content, retire it explicitly.
5. Re-run the write path until the memory module accepts the result.

Hard rules:

- Do not introduce compatibility shims, old fields, or silent fallback syntax.
- Do not change record kind casually.
- Do not rewrite a record when a narrow span repair is enough.
- Do not convert repair into fresh fact creation unless the existing record truly needs replacement.

Output should include:

- original ref
- fixed fields
- action
- final status
- remaining gap, if any
