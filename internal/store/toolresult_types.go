package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ToolResultStatus string

const (
	ToolResultStatusSucceeded ToolResultStatus = "succeeded"
	ToolResultStatusFailed    ToolResultStatus = "failed"
)

type SideEffectRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

const (
	SideEffectKindArtifact       = "artifact"
	SideEffectKindOperatorAction = "operator_action"
)

type EvidenceRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type ToolResultAppendRequest struct {
	RunID         string
	SessionID     string
	TurnIndex     int
	CallID        string
	ToolName      string
	ArgumentsJSON string
	Status        ToolResultStatus
	ErrorReason   string
	FullText      string
	TokenEstimate int
	SideEffects   []SideEffectRef
	EvidenceRefs  []EvidenceRef
	CreatedAt     time.Time
}

type ToolResultRecord struct {
	ResultRef     string
	RunID         string
	SessionID     string
	TurnIndex     int
	CallID        string
	ToolName      string
	ArgumentsJSON string
	Status        ToolResultStatus
	ErrorReason   string
	Preview       string
	FullText      string
	TokenEstimate int
	SideEffects   []SideEffectRef
	EvidenceRefs  []EvidenceRef
	CreatedAt     time.Time
}

var ErrToolResultNotFound = errors.New("tool result not found")

type ToolResultLedger interface {
	Append(context.Context, ToolResultAppendRequest) (ToolResultRecord, error)
	Load(context.Context, string) (ToolResultRecord, error)
	ListByRun(context.Context, string) ([]ToolResultRecord, error)
	AppendEvidenceRef(context.Context, string, EvidenceRef) (ToolResultRecord, error)
}

func BuildToolResultRef(runID string, callID string) string {
	return "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID)
}

func PreviewToolResult(text string, limit int) string {
	cleaned := strings.TrimSpace(text)
	if limit <= 0 || len(cleaned) <= limit {
		return cleaned
	}
	return strings.TrimSpace(cleaned[:limit]) + "..."
}

func NormalizeToolResultAppendRequest(req ToolResultAppendRequest) (ToolResultAppendRequest, error) {
	req.RunID = strings.TrimSpace(req.RunID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.CallID = strings.TrimSpace(req.CallID)
	req.ToolName = strings.TrimSpace(req.ToolName)
	req.ArgumentsJSON = strings.TrimSpace(req.ArgumentsJSON)
	req.ErrorReason = strings.TrimSpace(req.ErrorReason)
	var err error
	req.SideEffects, err = normalizeToolResultSideEffects(req.SideEffects)
	if err != nil {
		return ToolResultAppendRequest{}, err
	}
	req.EvidenceRefs = normalizeToolResultEvidenceRefs(req.EvidenceRefs)
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	} else {
		req.CreatedAt = req.CreatedAt.UTC()
	}
	if req.RunID == "" {
		return ToolResultAppendRequest{}, fmt.Errorf("tool result run_id is required")
	}
	if req.CallID == "" {
		return ToolResultAppendRequest{}, fmt.Errorf("tool result call_id is required")
	}
	if req.ToolName == "" {
		return ToolResultAppendRequest{}, fmt.Errorf("tool result tool_name is required")
	}
	switch req.Status {
	case ToolResultStatusSucceeded, ToolResultStatusFailed:
	default:
		return ToolResultAppendRequest{}, fmt.Errorf("tool result status %q is invalid", req.Status)
	}
	if req.TokenEstimate < 0 {
		return ToolResultAppendRequest{}, fmt.Errorf("tool result token estimate must be >= 0")
	}
	return req, nil
}

func NormalizeEvidenceRef(ref EvidenceRef) (EvidenceRef, error) {
	ref.Kind = strings.TrimSpace(ref.Kind)
	ref.Ref = strings.TrimSpace(ref.Ref)
	if ref.Kind == "" {
		return EvidenceRef{}, fmt.Errorf("tool result evidence ref kind is required")
	}
	if ref.Ref == "" {
		return EvidenceRef{}, fmt.Errorf("tool result evidence ref is required")
	}
	return ref, nil
}

func normalizeToolResultSideEffects(items []SideEffectRef) ([]SideEffectRef, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]SideEffectRef, 0, len(items))
	for _, item := range items {
		item.Kind = strings.TrimSpace(item.Kind)
		item.Ref = strings.TrimSpace(item.Ref)
		item.Path = strings.TrimSpace(item.Path)
		if item.Kind == "" && item.Ref == "" && item.Path == "" {
			continue
		}
		if item.Kind == "" {
			return nil, fmt.Errorf("tool result side effect kind is required")
		}
		switch item.Kind {
		case SideEffectKindArtifact, SideEffectKindOperatorAction:
			if item.Ref == "" {
				return nil, fmt.Errorf("tool result side effect %q requires ref", item.Kind)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeToolResultEvidenceRefs(items []EvidenceRef) []EvidenceRef {
	if len(items) == 0 {
		return nil
	}
	out := make([]EvidenceRef, 0, len(items))
	for _, item := range items {
		item.Kind = strings.TrimSpace(item.Kind)
		item.Ref = strings.TrimSpace(item.Ref)
		if item.Kind == "" && item.Ref == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
