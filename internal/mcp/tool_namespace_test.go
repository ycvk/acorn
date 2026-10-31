package mcp

import "testing"

func TestToolNamespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"alphanumeric", "github", "github"},
		{"with dash", "notion-mcp", "notion-mcp"},
		{"with spaces", "My Server", "My-Server"},
		{"empty", "", "provider"},
		{"special chars", "a!@#b", "a---b"},
		{"only special", "!@#", "provider"},
		{"underscore and dot", "my_srv.v2", "my-srv-v2"},
		{"non-ASCII", "本地服务", "provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolNamespace(tt.input)
			if got != tt.want {
				t.Errorf("ToolNamespace(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
