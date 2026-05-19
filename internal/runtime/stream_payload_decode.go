package runtime

import (
	"errors"
	"fmt"
	"strings"
)

func ElicitationPayloadFromStream(item StreamItem) (ElicitationPayload, error) {
	if item.Payload == nil {
		return ElicitationPayload{}, errors.New("elicitation payload is missing")
	}
	switch v := item.Payload.(type) {
	case ElicitationPayload:
		return validateElicitationPayload(v)
	case *ElicitationPayload:
		if v == nil {
			return ElicitationPayload{}, errors.New("elicitation payload is missing")
		}
		return validateElicitationPayload(*v)
	default:
		return ElicitationPayload{}, fmt.Errorf("elicitation payload has unexpected type %T", item.Payload)
	}
}

func SamplingPayloadFromStream(item StreamItem) (SamplingPayload, error) {
	if item.Payload == nil {
		return SamplingPayload{}, errors.New("sampling payload is missing")
	}
	switch v := item.Payload.(type) {
	case SamplingPayload:
		return validateSamplingPayload(v)
	case *SamplingPayload:
		if v == nil {
			return SamplingPayload{}, errors.New("sampling payload is missing")
		}
		return validateSamplingPayload(*v)
	default:
		return SamplingPayload{}, fmt.Errorf("sampling payload has unexpected type %T", item.Payload)
	}
}

func validateElicitationPayload(payload ElicitationPayload) (ElicitationPayload, error) {
	if strings.TrimSpace(payload.ActionID) == "" {
		return ElicitationPayload{}, errors.New("elicitation payload action_id is required")
	}
	return payload, nil
}

func validateSamplingPayload(payload SamplingPayload) (SamplingPayload, error) {
	if strings.TrimSpace(payload.RunID) == "" {
		return SamplingPayload{}, errors.New("sampling payload run_id is required")
	}
	return payload, nil
}
