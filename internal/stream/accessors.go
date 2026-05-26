package stream

// --- Typed accessor methods ---

func (item StreamItem) GetMessage() *StreamMessage {
	switch p := item.Payload.(type) {
	case *AssistantMessagePayload:
		return p.Message
	case *RunCompletedPayload:
		return p.Message
	default:
		return nil
	}
}

func (item StreamItem) GetAssistantDelta() *StreamAssistantDelta {
	if p, ok := item.Payload.(*AssistantDeltaPayload); ok {
		return p.AssistantDelta
	}
	return nil
}

func (item StreamItem) GetToolCall() *StreamToolCall {
	switch p := item.Payload.(type) {
	case *ToolCallStartedPayload:
		return p.ToolCall
	case *ToolCallSucceededPayload:
		return p.ToolCall
	case *ToolCallFailedPayload:
		return p.ToolCall
	case *ToolCallInterruptedPayload:
		return p.ToolCall
	default:
		return nil
	}
}

func (item StreamItem) GetToolCallProgress() *StreamToolCallProgress {
	if p, ok := item.Payload.(*ToolCallProgressPayload); ok {
		return p.ToolCall
	}
	return nil
}

func (item StreamItem) GetInterrupt() *StreamInterrupt {
	if p, ok := item.Payload.(*RunInterruptedPayload); ok {
		return p.Interrupt
	}
	return nil
}

func (item StreamItem) GetSkill() *StreamSkill {
	switch p := item.Payload.(type) {
	case *SkillDiscoveredPayload:
		return p.Skill
	case *SkillSelectedPayload:
		return p.Skill
	case *SkillLoadedPayload:
		return p.Skill
	case *SkillFailedPayload:
		return p.Skill
	default:
		return nil
	}
}

func (item StreamItem) GetSkillLifecycle() *StreamSkillLifecycle {
	if p, ok := item.Payload.(*SkillLifecyclePayload); ok {
		return p.SkillLifecycle
	}
	return nil
}

func (item StreamItem) GetMemoryPrepared() *StreamMemoryPrepared {
	if p, ok := item.Payload.(*MemoryPreparedPayload); ok {
		return p.MemoryPrepared
	}
	return nil
}

func (item StreamItem) GetProcedureActivation() *StreamProcedureActivation {
	if p, ok := item.Payload.(*ProcedureActivationPayload); ok {
		return p.ProcedureActivation
	}
	return nil
}

func (item StreamItem) GetContextCompressed() *StreamContextCompressed {
	if p, ok := item.Payload.(*ContextCompressedPayload); ok {
		return p.ContextCompressed
	}
	return nil
}

func (item StreamItem) GetContextPressure() *StreamContextPressure {
	if p, ok := item.Payload.(*ContextPressurePayload); ok {
		return p.ContextPressure
	}
	return nil
}

func (item StreamItem) GetError() string {
	if p, ok := item.Payload.(*RunFailedPayload); ok {
		return p.Error
	}
	return ""
}

func (item StreamItem) GetInput() string {
	if p, ok := item.Payload.(*RunStartedPayload); ok {
		return p.Input
	}
	return ""
}

func (item StreamItem) GetTargets() map[string]any {
	if p, ok := item.Payload.(*RunResumeRequestedPayload); ok {
		return p.Targets
	}
	return nil
}

func (item StreamItem) GetPlan() *StreamPlan {
	switch p := item.Payload.(type) {
	case *PlanCreatedPayload:
		return p.Plan
	case *PlanUpdatedPayload:
		return p.Plan
	case *PlanStepStartedPayload:
		return p.Plan
	case *PlanStepCompletedPayload:
		return p.Plan
	case *PlanStepFailedPayload:
		return p.Plan
	default:
		return nil
	}
}
