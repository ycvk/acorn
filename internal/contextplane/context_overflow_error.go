package contextplane

import (
	"errors"
	"strings"
)

var contextOverflowErrorMarkers = []string{
	"context_length_exceeded",
	"model_context_window_exceeded",
	"prompt_too_long",
	"prompt too long",
	"context window exceeded",
	"context length exceeded",
	"maximum context length",
	"max context length",
	"too many tokens",
	"input is too long",
	"exceeds the context window",
	"exceeded context window",
}

func IsContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	if contextOverflowText(err.Error()) {
		return true
	}
	var single interface{ Unwrap() error }
	if errors.As(err, &single) && single.Unwrap() != nil {
		return IsContextOverflowError(single.Unwrap())
	}
	var multi interface{ Unwrap() []error }
	if errors.As(err, &multi) {
		for _, child := range multi.Unwrap() {
			if IsContextOverflowError(child) {
				return true
			}
		}
	}
	return false
}

func contextOverflowText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range contextOverflowErrorMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
