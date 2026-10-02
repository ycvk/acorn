package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// Notifier sends or queues an owner notification.
type Notifier interface {
	Notify(ctx context.Context, n core.Notification) (core.Notification, error)
}

// NotifyToolDeps configure notify_owner. Without a Notifier the tool is
// registered as disabled and DisabledReason says why.
type NotifyToolDeps struct {
	Notifier       Notifier
	Context        core.ToolCallContextBridge
	Location       *time.Location
	DisabledReason string
}

// NotifyOwnerInput is the input of notify_owner.
type NotifyOwnerInput struct {
	Title string `json:"title" jsonschema_description:"A short title, a few words."`
	Body  string `json:"body" jsonschema_description:"One or two sentences. Push notifications pass through Google; keep details in the conversation."`
}

// NotifyOwnerOutput reports what happened to the notification.
type NotifyOwnerOutput struct {
	Status    string `json:"status"`
	SendAfter string `json:"send_after,omitempty"`
}

func buildNotifyOwnerTool(deps NotifyToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("notify_owner",
		"Send a push notification to the owner's phone. Inside quiet hours it waits until they end. Tapping it opens this conversation.",
		func(ctx context.Context, input NotifyOwnerInput, _ ToolProgressEmitter) (NotifyOwnerOutput, error) {
			title, body := strings.TrimSpace(input.Title), strings.TrimSpace(input.Body)
			if title == "" || body == "" {
				return NotifyOwnerOutput{}, errors.New("notify_owner: title and body are required")
			}
			n, err := deps.Notifier.Notify(ctx, core.Notification{
				Title:    title,
				Body:     body,
				ThreadID: deps.Context.CurrentSessionID(ctx),
				RunID:    deps.Context.CurrentRunID(ctx),
			})
			if err != nil {
				return NotifyOwnerOutput{}, fmt.Errorf("notify_owner: %w", err)
			}
			out := NotifyOwnerOutput{Status: string(n.Status)}
			if n.Status == core.NotificationQueued {
				out.SendAfter = n.SendAfter.In(deps.Location).Format(localTimeLayout)
			}
			return out, nil
		})
}
