# Acorn Decision Profile

This file is the canonical project decision contract for Acorn runs.
Only the active fenced blocks below are runtime-active.

## Defaults

```acorn-defaults
missing_context: inspect_first
missing_required_capability: block
```

## Routes

```acorn-routes
- intent: inspect
  action: execute_with_skill
  skill_id: skill.inspect.repo
- intent: debug
  action: execute_with_skill
  skill_id: skill.debug.backend
- intent: ship
  action: execute_with_skill
  skill_id: skill.ship.patch
```
