package runtime

import "github.com/ycvk/acorn/internal/domain"

func getPayloadMap(item domain.StreamItem) map[string]any {
	if item.Payload == nil {
		return nil
	}
	return item.Payload
}

func getNestedMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

func getInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

func getInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func getBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key].(bool)
	if !ok {
		return false
	}
	return v
}

func getStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func streamItemGetMessage(item domain.StreamItem) *StreamMessage {
	m := getPayloadMap(item)
	if msg, ok := m["message"].(*StreamMessage); ok && msg != nil {
		return msg
	}
	msgMap := getNestedMap(m, "message")
	if msgMap == nil {
		return nil
	}
	return &StreamMessage{
		Role:       getString(msgMap, "role"),
		Content:    getString(msgMap, "content"),
		Reasoning:  getString(msgMap, "reasoning"),
		ToolCallID: getString(msgMap, "tool_call_id"),
		ToolName:   getString(msgMap, "tool_name"),
	}
}

func streamItemGetAssistantDelta(item domain.StreamItem) *StreamAssistantDelta {
	m := getPayloadMap(item)
	if delta, ok := m["assistant_delta"].(*StreamAssistantDelta); ok && delta != nil {
		return delta
	}
	deltaMap := getNestedMap(m, "assistant_delta")
	if deltaMap == nil {
		return nil
	}
	return &StreamAssistantDelta{
		Role:      getString(deltaMap, "role"),
		Delta:     getString(deltaMap, "delta"),
		Reasoning: getString(deltaMap, "reasoning"),
		Sequence:  getInt(deltaMap, "sequence"),
		MessageID: getString(deltaMap, "message_id"),
		IsFinal:   getBool(deltaMap, "is_final"),
		Meta:      getNestedMap(deltaMap, "meta"),
	}
}

func streamItemGetToolCall(item domain.StreamItem) *StreamToolCall {
	m := getPayloadMap(item)
	if call, ok := m["tool_call"].(*StreamToolCall); ok && call != nil {
		return call
	}
	callMap := getNestedMap(m, "tool_call")
	if callMap == nil {
		return nil
	}
	return &StreamToolCall{
		Provider:          getString(callMap, "provider"),
		Name:              getString(callMap, "name"),
		CallID:            getString(callMap, "call_id"),
		ArgumentsJSON:     getString(callMap, "arguments_json"),
		InterruptID:       getString(callMap, "interrupt_id"),
		Output:            getString(callMap, "output"),
		Error:             getString(callMap, "error"),
		DurationMS:        getInt64(callMap, "duration_ms"),
		InterruptContexts: getInt(callMap, "interrupt_contexts"),
	}
}

func streamItemGetInterrupt(item domain.StreamItem) *StreamInterrupt {
	m := getPayloadMap(item)
	if interrupt, ok := m["interrupt"].(*StreamInterrupt); ok && interrupt != nil {
		return interrupt
	}
	interruptMap := getNestedMap(m, "interrupt")
	if interruptMap == nil {
		return nil
	}
	interrupt := &StreamInterrupt{
		ContextCount: getInt(interruptMap, "context_count"),
	}
	contextsRaw, ok := interruptMap["contexts"].([]any)
	if ok {
		interrupt.Contexts = make([]StreamInterruptContext, 0, len(contextsRaw))
		for _, ctxRaw := range contextsRaw {
			ctxMap, ok := ctxRaw.(map[string]any)
			if !ok {
				continue
			}
			interrupt.Contexts = append(interrupt.Contexts, StreamInterruptContext{
				ID:          getString(ctxMap, "id"),
				Address:     getString(ctxMap, "address"),
				Info:        compactInterruptInfo(ctxMap["info"]),
				IsRootCause: getBool(ctxMap, "is_root_cause"),
			})
		}
	}
	return interrupt
}

func streamItemGetSkill(item domain.StreamItem) *StreamSkill {
	m := getPayloadMap(item)
	if skill, ok := m["skill"].(*StreamSkill); ok && skill != nil {
		return skill
	}
	skillMap := getNestedMap(m, "skill")
	if skillMap == nil {
		return nil
	}
	skill := &StreamSkill{
		SelectedID:        getString(skillMap, "selected_id"),
		Name:              getString(skillMap, "name"),
		Source:            getString(skillMap, "source"),
		Origin:            getString(skillMap, "origin"),
		TaskPattern:       getString(skillMap, "task_pattern"),
		Path:              getString(skillMap, "path"),
		NoSelectionReason: getString(skillMap, "no_selection_reason"),
		Summary:           getString(skillMap, "summary"),
		Instruction:       getString(skillMap, "instruction"),
		Scripts:           getStringSlice(skillMap, "scripts"),
		Score:             getInt(skillMap, "score"),
		RunStatus:         getString(skillMap, "run_status"),
		PromotedFrom:      getString(skillMap, "promoted_from"),
		FailureReason:     getString(skillMap, "failure_reason"),
		MatchedTerms:      getStringSlice(skillMap, "matched_terms"),
	}
	reqMap := getNestedMap(skillMap, "requirements")
	if reqMap != nil {
		skill.Requirements = StreamSkillRequirements{
			Tools:    getStringSlice(reqMap, "tools"),
			Toolsets: getStringSlice(reqMap, "toolsets"),
			Bins:     getStringSlice(reqMap, "bins"),
			Env:      getStringSlice(reqMap, "env"),
		}
	}
	candidatesRaw, ok := skillMap["candidates"].([]any)
	if ok {
		skill.Candidates = make([]StreamSkillCandidate, 0, len(candidatesRaw))
		for _, candRaw := range candidatesRaw {
			candMap, ok := candRaw.(map[string]any)
			if !ok {
				continue
			}
			candidate := StreamSkillCandidate{
				ID:             getString(candMap, "id"),
				Name:           getString(candMap, "name"),
				Score:          getInt(candMap, "score"),
				FilteredReason: getString(candMap, "filtered_reason"),
				Summary:        getString(candMap, "summary"),
				Origin:         getString(candMap, "origin"),
				TaskPattern:    getString(candMap, "task_pattern"),
				MatchedTerms:   getStringSlice(candMap, "matched_terms"),
			}
			reqMap := getNestedMap(candMap, "requirements")
			if reqMap != nil {
				candidate.Requirements = StreamSkillRequirements{
					Tools:    getStringSlice(reqMap, "tools"),
					Toolsets: getStringSlice(reqMap, "toolsets"),
					Bins:     getStringSlice(reqMap, "bins"),
					Env:      getStringSlice(reqMap, "env"),
				}
			}
			skill.Candidates = append(skill.Candidates, candidate)
		}
	}
	return skill
}

func streamItemGetMemoryPrepared(item domain.StreamItem) *StreamMemoryPrepared {
	m := getPayloadMap(item)
	if mem, ok := m["memory_prepared"].(*StreamMemoryPrepared); ok && mem != nil {
		return mem
	}
	memMap := getNestedMap(m, "memory_prepared")
	if memMap == nil {
		return nil
	}
	mem := &StreamMemoryPrepared{
		Query:          getString(memMap, "query"),
		WorkspaceScope: getString(memMap, "workspace_scope"),
		NudgeCount:     getInt(memMap, "nudge_count"),
		EntryCount:     getInt(memMap, "entry_count"),
	}
	nudgesRaw, ok := memMap["nudges"].([]any)
	if ok {
		mem.Nudges = make([]StreamMemoryPreparedNudge, 0, len(nudgesRaw))
		for _, nudgeRaw := range nudgesRaw {
			nudgeMap, ok := nudgeRaw.(map[string]any)
			if !ok {
				continue
			}
			mem.Nudges = append(mem.Nudges, StreamMemoryPreparedNudge{
				Ref:    getString(nudgeMap, "ref"),
				Kind:   getString(nudgeMap, "kind"),
				Title:  getString(nudgeMap, "title"),
				Status: getString(nudgeMap, "status"),
				Reason: getString(nudgeMap, "reason"),
			})
		}
	}
	entriesRaw, ok := memMap["entries"].([]any)
	if ok {
		mem.Entries = make([]StreamMemoryPreparedEntry, 0, len(entriesRaw))
		for _, entryRaw := range entriesRaw {
			entryMap, ok := entryRaw.(map[string]any)
			if !ok {
				continue
			}
			mem.Entries = append(mem.Entries, StreamMemoryPreparedEntry{
				Ref:   getString(entryMap, "ref"),
				Kind:  getString(entryMap, "kind"),
				Title: getString(entryMap, "title"),
			})
		}
	}
	return mem
}

func streamItemGetError(item domain.StreamItem) string {
	return getString(getPayloadMap(item), "error")
}

func compactInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
