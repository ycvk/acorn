---
id: skill.memory.triage
name: Memory Triage
version: v1
category: native
summary: Decide whether an observation should become a durable fact, a reusable skill, or nothing, and route it accordingly.
trigger_hints:
  - remember this
  - memorize this
  - store this fact
  - should I remember this
  - 记住这个
  - 记忆分流
requires:
  tools:
    - memory_search
    - memory_read_file
---
# Memory Triage

Use this skill when deciding whether a new observation belongs in durable memory.

Work loop:

1. Classify the input as fact, reusable procedure, or do-not-store.
2. Search for an existing record before proposing any write.
3. Decide whether the durable target is a fact (`remember`), a generated skill (`skill_create`), or neither.
4. If the observation is already covered, prefer update or retire over duplication.
5. Hand off to `memory_fact_writer`, `skill_creator`, or `memory_repair` as appropriate.

Hard rules:

- Do not write memory in this skill.
- Do not turn transient chat, ephemeral debug noise, or run-only detail into durable memory unless it is stable and reusable.
- Do not route every user statement into memory; memory is for reusable facts and operational truths, and repeatable procedures belong in generated skills.
- Do not invent a new record schema.

Output should include:

- classification
- target (fact, skill, or none)
- existing ref or `none`
- recommended next skill
- rationale
