---
id: skill.creator
name: Skill Creator
version: v1
category: native
summary: Create or revise Acorn skills with precise triggers, supporting files, and durable evidence requirements.
lifecycle_status: verified
evidence_refs:
  - builtin:acorn-native-skill-seed-pack
trigger_hints:
  - create a skill
  - new skill
  - skill creator
  - write SKILL.md
  - 创建 skill
  - 生成技能
requires:
  tools:
    - read_file
    - create_file
    - replace_span
    - search_text
    - inspect_git_status
---
# Skill Creator

Use this skill when the task is to create, revise, split, or merge an Acorn skill.

Work loop:

1. Clarify the narrow job the skill should perform, the situations that should trigger it, and the situations that must not trigger it.
2. Inspect nearby existing skills and code paths before writing. Reuse the Acorn skill frontmatter schema instead of inventing a parallel manifest.
3. Draft `SKILL.md` with a concise description, precise `trigger_hints`, required tools, hard boundaries, and a short workflow the agent can actually follow.
4. Add supporting `references/`, `scripts/`, or `templates/` only when the skill needs concrete reusable assets. Do not bury core instructions in auxiliary files.
5. Leave generated skills in `lifecycle_status: draft` or `needs_eval` until a real `skill_assess` run provides evidence refs.

Hard rules:

- The LLM judges skill quality, but promotion requires builtin or durable evidence refs.
- A vague skill description is a bug because selection depends on it.
- Do not ask the user to decide whether the skill is good as the default path. User override is an explicit lifecycle action.
- Do not mark a generated skill `verified` without `evidence_refs`.
- Do not create hidden DB-only skills. Skill truth must be ordinary files.

Output should include:

- Skill directory and files created or changed
- Trigger and non-trigger cases
- Assessment evidence and lifecycle status
- Lifecycle status and required evidence before promotion
