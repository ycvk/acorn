package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

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
	Settings       core.SettingsReader
	DisabledReason string
}

// NotifyOwnerInput is the input of notify_owner.
type NotifyOwnerInput struct {
	Title      string `json:"title" jsonschema_description:"A short title, a few words."`
	Body       string `json:"body" jsonschema_description:"One or two sentences. Push notifications pass through Google; keep details in the conversation."`
	BrowserURL string `json:"browser_url,omitempty" jsonschema_description:"An http(s) page, such as a sign-in page, that a tap opens in your browser on the owner's phone, over this conversation. Use it when they need to sign in or act there."`
}

// NotifyOwnerOutput reports what happened to the notification.
type NotifyOwnerOutput struct {
	Status    string `json:"status"`
	SendAfter string `json:"send_after,omitempty"`
}

func buildNotifyOwnerTool(deps NotifyToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("notify_owner",
		"Send a push notification to the owner's phone. Inside quiet hours it waits until they end. Tapping it opens this conversation, or a page in your browser over it with browser_url.",
		func(ctx context.Context, input NotifyOwnerInput, _ ToolProgressEmitter) (NotifyOwnerOutput, error) {
			title, body := strings.TrimSpace(input.Title), strings.TrimSpace(input.Body)
			if title == "" || body == "" {
				return NotifyOwnerOutput{}, errors.New("notify_owner: title and body are required")
			}
			n := core.Notification{
				Title:    title,
				Body:     body,
				ThreadID: deps.Context.CurrentSessionID(ctx),
				RunID:    deps.Context.CurrentRunID(ctx),
			}
			if input.BrowserURL != "" {
				u, err := url.Parse(input.BrowserURL)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return NotifyOwnerOutput{}, fmt.Errorf("notify_owner: browser_url must be an http(s) URL, got %q", input.BrowserURL)
				}
				n.BrowserURL = input.BrowserURL
			}
			n, err := deps.Notifier.Notify(ctx, n)
			if err != nil {
				return NotifyOwnerOutput{}, fmt.Errorf("notify_owner: %w", err)
			}
			out := NotifyOwnerOutput{Status: string(n.Status)}
			loc, err := ownerLocation(ctx, deps.Settings)
			if err != nil {
				return NotifyOwnerOutput{}, fmt.Errorf("notify_owner: %w", err)
			}
			if n.Status == core.NotificationQueued {
				out.SendAfter = n.SendAfter.In(loc).Format(localTimeLayout)
			}
			return out, nil
		})
}
