package core

func getPayloadMap(item StreamItem) map[string]any {
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

func ItemGetMessage(item StreamItem) *StreamMessage {
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

func ItemGetAssistantDelta(item StreamItem) *StreamAssistantDelta {
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

func ItemGetInterrupt(item StreamItem) *StreamInterrupt {
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
				Info:        CompactInterruptInfo(ctxMap["info"]),
				IsRootCause: getBool(ctxMap, "is_root_cause"),
			})
		}
	}
	return interrupt
}

func ItemGetError(item StreamItem) string {
	return getString(getPayloadMap(item), "error")
}

// CompactInterruptInfo filters an interrupt info value down to the known
// interrupt payload keys, dropping everything else. Returns nil if the input
// is not a map or no known keys are present.
func CompactInterruptInfo(value any) any {
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
