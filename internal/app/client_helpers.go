package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

var (
	ErrClientProjectionFailed = errors.New("client projection failed")
	ErrClientNoPendingMessage = errors.New("client pending user message not found")
)

// projectionError wraps a format string into ErrClientProjectionFailed.
func projectionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClientProjectionFailed, fmt.Sprintf(format, args...))
}

func projectRunStatus(status events.RunStatus) (string, error) {
	switch status {
	case events.RunStatusRunning:
		return "running", nil
	case events.RunStatusSucceeded:
		return "completed", nil
	case events.RunStatusInterrupted:
		return "interrupted", nil
	case events.RunStatusFailed:
		return "failed", nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

func validateMessagePart(part MessagePart) error {
	switch part.Kind {
	case "text":
		if strings.TrimSpace(part.Text) == "" {
			return errors.New("text part requires text")
		}
	case "reasoning":
		if strings.TrimSpace(part.Reasoning) == "" {
			return errors.New("reasoning part requires reasoning")
		}
	case "work_status":
		switch part.Status {
		case "working", "interrupted", "failed":
		default:
			return fmt.Errorf("work_status part has unsupported status %q", part.Status)
		}
		if strings.TrimSpace(part.Title) == "" || strings.TrimSpace(part.Summary) == "" {
			return errors.New("work_status part requires title and summary")
		}
		if err := validateMessageAction(part.Action); err != nil {
			return fmt.Errorf("work_status part action: %w", err)
		}
	case "decision":
		if strings.TrimSpace(part.DecisionID) == "" || strings.TrimSpace(part.Question) == "" {
			return errors.New("decision part requires decision_id and question")
		}
		switch part.Status {
		case "", string(events.PendingActionStatusPending), string(events.PendingActionStatusApproved), string(events.PendingActionStatusRejected), string(events.PendingActionStatusResolved):
		default:
			return fmt.Errorf("decision part has unsupported status %q", part.Status)
		}
	case "result":
		if strings.TrimSpace(part.Title) == "" {
			return errors.New("result part requires title")
		}
	case "disclosure":
		if len(part.Items) == 0 {
			return errors.New("disclosure part requires items")
		}
		for index, item := range part.Items {
			if err := validateDisclosureItem(item); err != nil {
				return fmt.Errorf("disclosure part items[%d]: %w", index, err)
			}
		}
	case "technical_detail_link":
		if strings.TrimSpace(part.RunID) == "" && strings.TrimSpace(part.DetailRunID) == "" {
			return errors.New("technical_detail_link part requires run_id")
		}
	default:
		return fmt.Errorf("unsupported kind %q", part.Kind)
	}
	return nil
}

func validateDisclosureItem(item DisclosureItem) error {
	switch item.Kind {
	case "memory", "skill":
	default:
		return fmt.Errorf("unsupported kind %q", item.Kind)
	}
	if strings.TrimSpace(item.Label) == "" {
		return errors.New("label is required")
	}
	switch item.Tone {
	case "memory", "skill", "procedure", "neutral", "warning":
	default:
		return fmt.Errorf("unsupported tone %q", item.Tone)
	}
	if strings.TrimSpace(item.SkillID) != "" && item.Kind != "skill" {
		return errors.New("skill_id is only supported for skill disclosure items")
	}
	return nil
}

func validateMessageAction(action *MessageAction) error {
	if action == nil {
		return nil
	}
	switch action.Kind {
	case "resume_run":
	default:
		return fmt.Errorf("unsupported kind %q", action.Kind)
	}
	if strings.TrimSpace(action.RunID) == "" {
		return errors.New("run_id is required")
	}
	if strings.TrimSpace(action.Label) == "" {
		return errors.New("label is required")
	}
	return nil
}

// Thread is a user-facing thread DTO.
type Thread struct {
	ID            string
	Title         string
	WorkspaceRoot string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LatestRunID   string
	State         string
}

// Message is a user-facing message DTO.
type Message struct {
	ID        string
	ThreadID  string
	Role      string
	Content   MessageContent
	CreatedAt time.Time
	RunID     string
}

// MessageContent holds the content of a message.
type MessageContent struct {
	Type  string
	Text  string
	Parts []MessagePart
}

// MessagePart is a single part of a message content.
type MessagePart struct {
	Kind             string           `json:"kind"`
	Text             string           `json:"text,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	Status           string           `json:"status,omitempty"`
	Title            string           `json:"title,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Changed          []string         `json:"changed,omitempty"`
	Verified         []string         `json:"verified,omitempty"`
	Risks            []string         `json:"risks,omitempty"`
	Items            []DisclosureItem `json:"items,omitempty"`
	DetailRunID      string           `json:"detail_run_id,omitempty"`
	RunID            string           `json:"run_id,omitempty"`
	Label            string           `json:"label,omitempty"`
	DecisionID       string           `json:"decision_id,omitempty"`
	Question         string           `json:"question,omitempty"`
	SelectedOptionID string           `json:"selected_option_id,omitempty"`
	Answer           string           `json:"answer,omitempty"`
	Options          []DecisionOption `json:"options,omitempty"`
	Action           *MessageAction   `json:"action,omitempty"`
}

// DisclosureItem is an item inside a disclosure message part.
type DisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

// DecisionOption is an option inside a decision message part.
type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// MessageAction is an action associated with a message part.
type MessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

// Run is a user-facing run DTO.
type Run struct {
	ID          string
	ThreadID    string
	SkillID     string
	Status      string
	Mode        string
	CreatedAt   time.Time
	CompletedAt time.Time
}
