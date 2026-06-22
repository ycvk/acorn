package stream

type StreamItemKind string

const (
	StreamKindRunStarted          StreamItemKind = "run_started"
	StreamKindRunCompleted        StreamItemKind = "run_completed"
	StreamKindRunFailed           StreamItemKind = "run_failed"
	StreamKindRunInterrupted      StreamItemKind = "run_interrupted"
	StreamKindRunResumeRequested  StreamItemKind = "run_resume_requested"
	StreamKindDecisionBlocked     StreamItemKind = "decision_blocked"
	StreamKindSkillDiscovered     StreamItemKind = "skill_discovered"
	StreamKindSkillSelected       StreamItemKind = "skill_selected"
	StreamKindSkillLoaded         StreamItemKind = "skill_loaded"
	StreamKindSkillFailed         StreamItemKind = "skill_failed"
	StreamKindProcedureActivation StreamItemKind = "procedure.activation"
	StreamKindMemoryPrepared      StreamItemKind = "memory_prepared"
	StreamKindAssistantDelta      StreamItemKind = "assistant.delta"
	StreamKindAssistantMessage    StreamItemKind = "assistant_message"
	StreamKindToolCallStarted     StreamItemKind = "tool_call_started"
	StreamKindToolCallSucceeded   StreamItemKind = "tool_call_succeeded"
	StreamKindToolCallFailed      StreamItemKind = "tool_call_failed"
	StreamKindToolCallInterrupted StreamItemKind = "tool_call_interrupted"
	StreamKindElicitationPending  StreamItemKind = "elicitation.pending"
	StreamKindElicitationDecided  StreamItemKind = "elicitation.decided"
	StreamKindSubagentStarted     StreamItemKind = "subagent.started"
	StreamKindSubagentCompleted   StreamItemKind = "subagent.completed"
	StreamKindSubagentFailed      StreamItemKind = "subagent.failed"
)
