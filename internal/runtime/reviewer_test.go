package runtime

import (
	"testing"
)

func TestParseFacts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "single fact",
			input: `FACT: Owner timezone | UTC+8`,
			want:  1,
		},
		{
			name: "multiple facts with noise",
			input: `Here are the facts:
FACT: Owner name | Alice
Some commentary.
FACT: Server location | Tokyo, JP
NOTHING`,
			want: 2,
		},
		{
			name:  "nothing",
			input: `NOTHING`,
			want:  0,
		},
		{
			name:  "malformed fact missing pipe",
			input: `FACT: no pipe here`,
			want:  0,
		},
		{
			name: "empty title or body",
			input: `FACT:  | body
FACT: title | 
FACT: good | fact`,
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFacts(tt.input)
			if len(got) != tt.want {
				t.Fatalf("parseFacts got %d facts, want %d; got=%+v", len(got), tt.want, got)
			}
		})
	}
}

func TestParseFactsContent(t *testing.T) {
	facts := parseFacts("FACT: Owner timezone | UTC+8")
	if len(facts) != 1 {
		t.Fatalf("want 1 fact, got %d", len(facts))
	}
	if facts[0].title != "Owner timezone" {
		t.Errorf("title = %q, want %q", facts[0].title, "Owner timezone")
	}
	if facts[0].body != "UTC+8" {
		t.Errorf("body = %q, want %q", facts[0].body, "UTC+8")
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"cjk", "你好世界测试", 4, "你好世界..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateRunes(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}
