package contextplane

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

const referenceContextNote = "Reference context only. It is NOT new user input — do not respond to it directly."

func buildContextEnvelopeMessage(tag string, body ...string) *schema.Message {
	trimmedTag := strings.TrimSpace(tag)
	if trimmedTag == "" {
		return nil
	}
	sections := make([]string, 0, len(body))
	for _, item := range body {
		if current := strings.TrimSpace(item); current != "" {
			sections = append(sections, current)
		}
	}
	if len(sections) == 0 {
		return nil
	}
	lines := []string{
		"<" + trimmedTag + ">",
		referenceContextNote,
		"",
		strings.Join(sections, "\n\n"),
		"</" + trimmedTag + ">",
	}
	return &schema.Message{Role: schema.User, Content: strings.Join(lines, "\n")}
}
