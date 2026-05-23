package runprojection

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/runstream"
)

func ElicitationPayloadFromStream(item StreamItem) (ElicitationPayload, error) {
	if item.Payload == nil {
		return ElicitationPayload{}, errors.New("elicitation payload is missing")
	}
	switch v := item.Payload.(type) {
	case ElicitationPayload:
		payload, err := ValidateElicitationPayload(v)
		if err != nil {
			return ElicitationPayload{}, err
		}
		return runstream.ElicitationPayloadWithStreamKind(payload, item.Kind), nil
	case *ElicitationPayload:
		if v == nil {
			return ElicitationPayload{}, errors.New("elicitation payload is missing")
		}
		payload, err := ValidateElicitationPayload(*v)
		if err != nil {
			return ElicitationPayload{}, err
		}
		return runstream.ElicitationPayloadWithStreamKind(payload, item.Kind), nil
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
		payload, err := ValidateSamplingPayload(v)
		if err != nil {
			return SamplingPayload{}, err
		}
		return runstream.SamplingPayloadWithStreamKind(payload, item.Kind), nil
	case *SamplingPayload:
		if v == nil {
			return SamplingPayload{}, errors.New("sampling payload is missing")
		}
		payload, err := ValidateSamplingPayload(*v)
		if err != nil {
			return SamplingPayload{}, err
		}
		return runstream.SamplingPayloadWithStreamKind(payload, item.Kind), nil
	default:
		return SamplingPayload{}, fmt.Errorf("sampling payload has unexpected type %T", item.Payload)
	}
}

func ValidateElicitationPayload(payload ElicitationPayload) (ElicitationPayload, error) {
	if strings.TrimSpace(payload.ActionID) == "" {
		return ElicitationPayload{}, errors.New("elicitation payload action_id is required")
	}
	return payload, nil
}

func ValidateSamplingPayload(payload SamplingPayload) (SamplingPayload, error) {
	if strings.TrimSpace(payload.RunID) == "" {
		return SamplingPayload{}, errors.New("sampling payload run_id is required")
	}
	return payload, nil
}
