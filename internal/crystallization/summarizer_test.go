package crystallization

import (
	"testing"
)

func TestDefaultSummarizer(t *testing.T) {
	s := NewSummarizer()

	cases := []struct {
		name        string
		title       string
		body        string
		taskPattern string
		toolNames   []string
		want        string
	}{
		{
			name:        "all fields",
			title:       "Test Title",
			body:        "This is a long body that describes the procedure in detail.",
			taskPattern: "test pattern",
			toolNames:   []string{"read_file", "write_file"},
			want:        "Pattern: test pattern; Tools: read_file, write_file; This is a long body that describes the procedure in detail.",
		},
		{
			name:        "empty fields",
			title:       "",
			body:        "",
			taskPattern: "",
			toolNames:   nil,
			want:        "",
		},
		{
			name:        "only body truncated",
			title:       "",
			body:        "This is a very long body text that definitely exceeds the one hundred and fifty character limit for summaries so it must be truncated with ellipsis at the end of the limit here.",
			taskPattern: "",
			toolNames:   nil,
			want:        "This is a very long body text that definitely exceeds the one hundred and fifty character limit for summaries so it must be truncated with ellipsis at...",
		},
		{
			name:        "only pattern",
			title:       "",
			body:        "",
			taskPattern: "search and replace",
			toolNames:   nil,
			want:        "Pattern: search and replace",
		},
		{
			name:        "only tools",
			title:       "",
			body:        "",
			taskPattern: "",
			toolNames:   []string{"git_status", "git_diff"},
			want:        "Tools: git_status, git_diff",
		},
		{
			name:        "result truncated at 200",
			title:       "",
			body:        "",
			taskPattern: "an extremely long task pattern that combined with a long tools list will definitely exceed the two hundred character limit for the final summary result",
			toolNames:   []string{"tool_one", "tool_two", "tool_three", "tool_four", "tool_five", "tool_six"},
			want:        "Pattern: an extremely long task pattern that combined with a long tools list will definitely exceed the two hundred character limit for the final summary result; Tools: tool_one, tool_two, tool_three,",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := s.Summarize(tc.title, tc.body, tc.taskPattern, tc.toolNames)
			if got != tc.want {
				t.Fatalf("Summarize() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultSummarizerNil(t *testing.T) {
	var s *DefaultSummarizer
	if got := s.Summarize("title", "body", "pattern", []string{"t"}); got != "" {
		t.Fatalf("nil summarizer = %q, want empty", got)
	}
}
