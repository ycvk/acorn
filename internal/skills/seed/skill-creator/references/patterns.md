# Skill patterns

Adapted from the skill-creator skill, released under the Apache License 2.0; see LICENSE.txt.

## Steps

Give an overview of a multi-step process near the top:

```markdown
Filling a PDF form takes these steps:

1. Analyze the form (run scripts/analyze_form.py)
2. Map the fields (edit fields.json)
3. Check the mapping (run scripts/check_fields.py)
4. Fill the form (run scripts/fill_form.py)
5. Verify the output
```

For branching work, name the decision first:

```markdown
1. Decide what kind of change it is:
   - New content: follow "Creating" below
   - Changing existing content: follow "Editing" below
```

## Output

When the result must have a fixed shape, show it and say how strict it is:

```markdown
## Report

Always use this structure:

# [Title]
## Summary
[One paragraph]
## Findings
- Finding with its data
```

When style matters more than shape, show input and output pairs:

```markdown
Input: Added sign-in with tokens
Output: feat(auth): sign in with tokens
```

## Splitting into files

Keep the core steps and the choice between variants in SKILL.md; move details of each variant into its own file under `references/`, and say in SKILL.md when to read it:

```
cloud-deploy/
├── SKILL.md            steps + choosing a provider
└── references/
    ├── aws.md
    └── gcp.md
```

- Keep references one level deep from SKILL.md.
- Give a reference longer than about 100 lines a short table of contents at the top.
- Keep each fact in one place: SKILL.md or a reference, not both.
