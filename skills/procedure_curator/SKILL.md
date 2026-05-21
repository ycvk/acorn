---
id: skill.procedure.curator
name: Procedure Curator
version: v1
category: native
summary: Curate file-backed memory procedures from run evidence as the procedure branch of the memory skill family without restoring hidden candidate review flows.
lifecycle_status: verified
evidence_refs:
  - builtin:acorn-native-skill-seed-pack
trigger_hints:
  - curate procedure
  - update learned procedure
  - retire procedure
  - action verified procedure
  - memory procedure
  - procedure memory
  - 维护 procedure
  - 更新流程记忆
requires:
  tools:
    - memory_search
    - memory_read_file
    - memory_create_file
    - memory_replace_span
---
# Procedure Curator

Use this skill when maintaining `memorymodule/skills/` procedure records from concrete run evidence.

Work loop:

1. Locate the current procedure record with memory search or direct file read.
2. Check provenance before changing anything: `origin`, `status`, `source_run`, `source_refs`, and `evidence_refs` must match the ProcedureRecord contract.
3. For a new or uncertain procedure, write `origin: agent_draft` and `status: unverified`.
4. Promote only action-verified procedures that cite a real source run and evidence refs.
5. Patch, split, merge, downgrade, or retire procedures based on evidence. Keep the file history readable.
6. Report the lifecycle decision with reason, affected refs, and remaining evidence gap if the result is still draft or needs eval.

Hard rules:

- Do not reintroduce MemoryCandidate, AdmissionGate, reflection distillation, closeout learner, or a hidden review inbox.
- Do not mark a procedure verified because it sounds plausible.
- Do not convert executable native skills and memory procedures into one file model.
- User confirmation is not the normal quality gate; user override is explicit and auditable.

Output should include:

- Procedure ref
- Curation action
- Evidence refs used
- File change summary
- Resulting status
