package toolresult

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
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

type AppendRequest struct {
	RunID         string
	SessionID     string
	TurnIndex     int
	CallID        string
	ToolName      string
	ArgumentsJSON string
	Status        Status
	ErrorReason   string
	FullText      string
	TokenEstimate int
	SideEffects   []SideEffectRef
	EvidenceRefs  []EvidenceRef
	CreatedAt     time.Time
}

type Record struct {
	ResultRef     string
	RunID         string
	SessionID     string
	TurnIndex     int
	CallID        string
	ToolName      string
	ArgumentsJSON string
	Status        Status
	ErrorReason   string
	Preview       string
	FullText      string
	TokenEstimate int
	SideEffects   []SideEffectRef
	EvidenceRefs  []EvidenceRef
	CreatedAt     time.Time
}

var ErrToolResultNotFound = errors.New("tool result not found")

type Ledger interface {
	Append(context.Context, AppendRequest) (Record, error)
	Load(context.Context, string) (Record, error)
	ListByRun(context.Context, string) ([]Record, error)
	AppendEvidenceRef(context.Context, string, EvidenceRef) (Record, error)
}

func BuildRef(runID string, callID string) string {
	return "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID)
}

func Preview(text string, limit int) string {
	cleaned := strings.TrimSpace(text)
	if limit <= 0 || len(cleaned) <= limit {
		return cleaned
	}
	return strings.TrimSpace(cleaned[:limit]) + "..."
}

func NormalizeAppendRequest(req AppendRequest) (AppendRequest, error) {
	req.RunID = strings.TrimSpace(req.RunID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.CallID = strings.TrimSpace(req.CallID)
	req.ToolName = strings.TrimSpace(req.ToolName)
	req.ArgumentsJSON = strings.TrimSpace(req.ArgumentsJSON)
	req.ErrorReason = strings.TrimSpace(req.ErrorReason)
	var err error
	req.SideEffects, err = normalizeSideEffects(req.SideEffects)
	if err != nil {
		return AppendRequest{}, err
	}
	req.EvidenceRefs = normalizeEvidenceRefs(req.EvidenceRefs)
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	} else {
		req.CreatedAt = req.CreatedAt.UTC()
	}
	if req.RunID == "" {
		return AppendRequest{}, fmt.Errorf("tool result run_id is required")
	}
	if req.CallID == "" {
		return AppendRequest{}, fmt.Errorf("tool result call_id is required")
	}
	if req.ToolName == "" {
		return AppendRequest{}, fmt.Errorf("tool result tool_name is required")
	}
	switch req.Status {
	case StatusSucceeded, StatusFailed:
	default:
		return AppendRequest{}, fmt.Errorf("tool result status %q is invalid", req.Status)
	}
	if req.TokenEstimate < 0 {
		return AppendRequest{}, fmt.Errorf("tool result token estimate must be >= 0")
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

func normalizeSideEffects(items []SideEffectRef) ([]SideEffectRef, error) {
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

func normalizeEvidenceRefs(items []EvidenceRef) []EvidenceRef {
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
