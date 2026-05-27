package contextplane

import (
	"regexp"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const TurnIndexExtraKey = "acorn_turn_index"

const (
	CompressionSummaryMarkerKey   = "acorn.context_compression.kind"
	CompressionSummaryMarkerValue = "summary"
)

var (
	privateKeyBlockRe = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	apiKeyTokenRe     = regexp.MustCompile(`(?i)\b(?:sk-proj|sk-ant|sk-live|sk-test|sk|ak)-[A-Za-z0-9_\-]{3,}\b`)
	bearerTokenRe     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	secretKeyValueRe  = regexp.MustCompile(`(?i)\b(api_key|apikey|api_secret|secret_key|access_key|access_token|auth_token|password|passwd|credential|private_key)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s"',;]+)`)
	apiKeyHeaderRe    = regexp.MustCompile(`(?im)\b(Authorization|X-Api-Key|X-Auth-Token)(\s*:\s*)([^\r\n]+)`)
	uriPasswordRe     = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@\s/]+)(@)`)
	querySecretRe     = regexp.MustCompile(`(?i)([?&](?:api_key|apikey|access_token|auth_token|token|password|secret)=)([^&#\s]+)`)
)

func CloneMessage(msg adk.Message) *schema.Message {
	message := *msg
	if msg.Extra != nil {
		message.Extra = CloneAnyMap(msg.Extra)
	}
	if msg.UserInputMultiContent != nil {
		message.UserInputMultiContent = append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...)
		for i := range message.UserInputMultiContent {
			if message.UserInputMultiContent[i].Extra != nil {
				message.UserInputMultiContent[i].Extra = CloneAnyMap(message.UserInputMultiContent[i].Extra)
			}
		}
	}
	if msg.AssistantGenMultiContent != nil {
		message.AssistantGenMultiContent = append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...)
		for i := range message.AssistantGenMultiContent {
			if message.AssistantGenMultiContent[i].Extra != nil {
				message.AssistantGenMultiContent[i].Extra = CloneAnyMap(message.AssistantGenMultiContent[i].Extra)
			}
		}
	}
	if msg.ToolCalls != nil {
		message.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
	}
	return &message
}

func CloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func MarkCompressionSummary(msg adk.Message) adk.Message {
	if msg == nil {
		return nil
	}
	message := CloneMessage(msg)
	if message.Extra == nil {
		message.Extra = map[string]any{}
	}
	message.Extra[CompressionSummaryMarkerKey] = CompressionSummaryMarkerValue
	return message
}

func SanitizeSummaryMessage(msg adk.Message) adk.Message {
	if msg == nil {
		return nil
	}
	message := CloneMessage(msg)
	message.Content = RedactSecrets(message.Content)
	for i := range message.UserInputMultiContent {
		message.UserInputMultiContent[i].Text = RedactSecrets(message.UserInputMultiContent[i].Text)
	}
	for i := range message.AssistantGenMultiContent {
		message.AssistantGenMultiContent[i].Text = RedactSecrets(message.AssistantGenMultiContent[i].Text)
		if message.AssistantGenMultiContent[i].Reasoning != nil {
			reasoning := *message.AssistantGenMultiContent[i].Reasoning
			reasoning.Text = RedactSecrets(reasoning.Text)
			message.AssistantGenMultiContent[i].Reasoning = &reasoning
		}
	}
	return message
}

func CompactionSummaryMessage(summaryText string) adk.Message {
	return &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{
				Type: schema.ChatMessagePartTypeText,
				Text: summaryText,
			},
			{
				Type: schema.ChatMessagePartTypeText,
				Text: "Continue the conversation from this context checkpoint. Do not ask the user to repeat already summarized information.",
			},
		},
	}
}

func RedactSecrets(s string) string {
	result := s
	result = privateKeyBlockRe.ReplaceAllString(result, `[REDACTED:private-key]`)
	result = apiKeyHeaderRe.ReplaceAllString(result, `${1}${2}[REDACTED]`)
	result = bearerTokenRe.ReplaceAllString(result, `Bearer [REDACTED]`)
	result = apiKeyTokenRe.ReplaceAllString(result, `[REDACTED:api-key]`)
	result = secretKeyValueRe.ReplaceAllString(result, `${1}${2}[REDACTED]`)
	result = uriPasswordRe.ReplaceAllString(result, `${1}[REDACTED]${3}`)
	result = querySecretRe.ReplaceAllString(result, `${1}[REDACTED]`)
	return result
}
