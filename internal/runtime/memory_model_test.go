package runtime

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/schema/claude"
	"github.com/cloudwego/eino/schema/openai"
)

func TestOutputCutOffReadsProviderStopSignals(t *testing.T) {
	for name, tc := range map[string]struct {
		meta *schema.AgenticResponseMeta
		want string
	}{
		"no meta":             {nil, ""},
		"responses completed": {&schema.AgenticResponseMeta{OpenAIExtension: &openai.ResponseMetaExtension{Status: openai.ResponseStatusCompleted}}, ""},
		"responses cut off":   {&schema.AgenticResponseMeta{OpenAIExtension: &openai.ResponseMetaExtension{Status: openai.ResponseStatusIncomplete, IncompleteDetails: &openai.IncompleteDetails{Reason: "max_output_tokens"}}}, "max_output_tokens"},
		"claude finished":     {&schema.AgenticResponseMeta{ClaudeExtension: &claude.ResponseMetaExtension{StopReason: "end_turn"}}, ""},
		"claude cut off":      {&schema.AgenticResponseMeta{ClaudeExtension: &claude.ResponseMetaExtension{StopReason: "max_tokens"}}, "max_tokens"},
	} {
		if got := outputCutOff(tc.meta); got != tc.want {
			t.Errorf("%s: outputCutOff = %q, want %q", name, got, tc.want)
		}
	}
}
