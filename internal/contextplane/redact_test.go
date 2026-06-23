package contextplane

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestRedactSecretsPrivateKeyBlock(t *testing.T) {
	input := "config: " + `-----BEGIN RSA PRIVATE KEY-----` + "\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----" + " done"
	got := RedactSecrets(input)
	if strings.Contains(got, "MIIEpAIBAAKCAQEA") {
		t.Error("private key material not redacted")
	}
	if !strings.Contains(got, "[REDACTED:private-key]") {
		t.Error("expected [REDACTED:private-key] marker")
	}
}

func TestRedactSecretsAPIKeyHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"authorization header", "Authorization: Bearer sk-proj-abc123XYZ"},
		{"x-api-key header", "X-Api-Key: sk-live-secretkey456"},
		{"x-auth-token header", "X-Auth-Token: some-token-value-here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			// The header value after the colon must be redacted.
			if strings.Contains(got, "sk-proj-abc123XYZ") {
				t.Errorf("API key leaked: %q", got)
			}
			if strings.Contains(got, "sk-live-secretkey456") {
				t.Errorf("API key leaked: %q", got)
			}
			if strings.Contains(got, "some-token-value-here") {
				t.Errorf("auth token leaked: %q", got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("expected [REDACTED] marker in %q", got)
			}
		})
	}
}

func TestRedactSecretsBearerToken(t *testing.T) {
	input := "token is Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload here"
	got := RedactSecrets(input)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Error("bearer token payload not redacted")
	}
	if !strings.Contains(got, "Bearer [REDACTED]") {
		t.Errorf("expected 'Bearer [REDACTED]', got %q", got)
	}
}

func TestRedactSecretsAPIKeyToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		token string
	}{
		{"sk-proj prefix", "key is sk-proj-abc123XYZ here", "abc123XYZ"},
		{"sk-ant prefix", "key is sk-ant-mysecretkey789 here", "mysecretkey789"},
		{"sk-live prefix", "key is sk-live-livesecret12 here", "livesecret12"},
		{"sk-test prefix", "key is sk-test-testsecret345 here", "testsecret345"},
		{"sk bare prefix", "key is sk-baresecret999 here", "baresecret999"},
		{"ak prefix", "key is ak-anotherkey456 here", "anotherkey456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			if strings.Contains(got, tt.token) {
				t.Errorf("token %q leaked in %q", tt.token, got)
			}
			if !strings.Contains(got, "[REDACTED:api-key]") {
				t.Errorf("expected [REDACTED:api-key] marker in %q", got)
			}
		})
	}
}

func TestRedactSecretsSecretKeyValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		leak  string
	}{
		{"api_key equals", `config api_key=mysecretvalue123 end`, "mysecretvalue123"},
		{"api_key colon", `config api_key: "mysecretvalue123" end`, "mysecretvalue123"},
		{"password equals", `password=hunter2end`, "hunter2"},
		{"passwd quoted", `passwd: 'hunter2'`, "hunter2"},
		{"secret_key equals", `secret_key=topsecret789`, "topsecret789"},
		{"access_token equals", `access_token=tokenval456`, "tokenval456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			if strings.Contains(got, tt.leak) {
				t.Errorf("secret value %q leaked in %q", tt.leak, got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("expected [REDACTED] marker in %q", got)
			}
		})
	}
}

func TestRedactSecretsURIPassword(t *testing.T) {
	input := "connect to https://user:secretpass@db.example.com:5432/db"
	got := RedactSecrets(input)
	if strings.Contains(got, "secretpass") {
		t.Errorf("URI password leaked in %q", got)
	}
	if !strings.Contains(got, "db.example.com") {
		t.Errorf("non-secret URI host should be preserved in %q", got)
	}
}

func TestRedactSecretsQuerySecret(t *testing.T) {
	tests := []struct {
		name  string
		input string
		leak  string
	}{
		{"api_key param", "GET https://api.example.com/data?api_key=secretkey123&format=json", "secretkey123"},
		{"access_token param", "url?access_token=tok123&foo=bar", "tok123"},
		{"password param", "url?password=pw456", "pw456"},
		{"token param", "url?token=tok789", "tok789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			if strings.Contains(got, tt.leak) {
				t.Errorf("query secret %q leaked in %q", tt.leak, got)
			}
		})
	}
}

func TestRedactSecretsPreservesNonSensitiveText(t *testing.T) {
	input := "The user asked to read file /tmp/data.json and run command ls -la"
	got := RedactSecrets(input)
	if got != input {
		t.Errorf("non-sensitive text was modified: %q", got)
	}
}

func TestRedactSecretsEmptyString(t *testing.T) {
	if got := RedactSecrets(""); got != "" {
		t.Errorf("empty input = %q, want empty", got)
	}
}

func TestRedactSecretsMultipleSecretsInOneString(t *testing.T) {
	input := "config: api_key=secret123 token=Bearer abc.def.ghi url=https://admin:p4ss@host"
	got := RedactSecrets(input)
	for _, leak := range []string{"secret123", "abc.def.ghi", "p4ss"} {
		if strings.Contains(got, leak) {
			t.Errorf("secret %q leaked in %q", leak, got)
		}
	}
}

func TestSanitizeSummaryMessageRedactsContent(t *testing.T) {
	msg := schema.AssistantMessage("api_key=secret123 end", nil)
	sanitized := SanitizeSummaryMessage(msg)
	if sanitized == nil {
		t.Fatal("sanitized message is nil")
	}
	if strings.Contains(sanitized.Content, "secret123") {
		t.Errorf("secret leaked in content: %q", sanitized.Content)
	}
}

func TestSanitizeSummaryMessageRedactsMultiContent(t *testing.T) {
	msg := &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "api_key=secret123 end"},
			{Type: schema.ChatMessagePartTypeText, Text: "password=hunter2 end"},
		},
	}
	sanitized := SanitizeSummaryMessage(msg)
	for _, part := range sanitized.UserInputMultiContent {
		if strings.Contains(part.Text, "secret123") {
			t.Errorf("secret leaked in multi-content: %q", part.Text)
		}
		if strings.Contains(part.Text, "hunter2") {
			t.Errorf("password leaked in multi-content: %q", part.Text)
		}
	}
}

func TestSanitizeSummaryMessageRedactsReasoning(t *testing.T) {
	msg := &schema.Message{
		Role: schema.Assistant,
		AssistantGenMultiContent: []schema.MessageOutputPart{
			{
				Type:      schema.ChatMessagePartTypeText,
				Text:      "safe text",
				Reasoning: &schema.MessageOutputReasoning{Text: "token=Bearer abc.def.ghi"},
			},
		},
	}
	sanitized := SanitizeSummaryMessage(msg)
	for _, part := range sanitized.AssistantGenMultiContent {
		if part.Reasoning != nil && strings.Contains(part.Reasoning.Text, "abc.def.ghi") {
			t.Errorf("secret leaked in reasoning: %q", part.Reasoning.Text)
		}
	}
}

func TestSanitizeSummaryMessageNilReturnsNil(t *testing.T) {
	if got := SanitizeSummaryMessage(nil); got != nil {
		t.Errorf("nil input = %v, want nil", got)
	}
}

func TestSanitizeSummaryMessageDoesNotMutateOriginal(t *testing.T) {
	original := schema.AssistantMessage("api_key=secret123 end", nil)
	_ = SanitizeSummaryMessage(original)
	if original.Content != "api_key=secret123 end" {
		t.Errorf("original message was mutated: %q", original.Content)
	}
}
