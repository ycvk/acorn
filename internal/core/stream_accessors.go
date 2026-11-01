package core

// The runtime puts typed values in a stream item's payload; these read them
// back.

func ItemGetMessage(item StreamItem) *StreamMessage {
	if msg, ok := item.Payload["message"].(*StreamMessage); ok {
		return msg
	}
	return nil
}

func ItemGetAssistantDelta(item StreamItem) *StreamAssistantDelta {
	if delta, ok := item.Payload["assistant_delta"].(*StreamAssistantDelta); ok {
		return delta
	}
	return nil
}

func ItemGetInterrupt(item StreamItem) *StreamInterrupt {
	if interrupt, ok := item.Payload["interrupt"].(*StreamInterrupt); ok {
		return interrupt
	}
	return nil
}

func ItemGetError(item StreamItem) string {
	if text, ok := item.Payload["error"].(string); ok {
		return text
	}
	return ""
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
