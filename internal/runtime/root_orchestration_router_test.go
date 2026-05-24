package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/orchestrationmode"
)

func TestResolveRootOrchestrationMode(t *testing.T) {
	tests := []struct {
		name string
		req  ExecuteRequest
		want orchestrationmode.Mode
	}{
		{
			name: "greeting uses direct response",
			req:  ExecuteRequest{Input: "你好"},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "english greeting uses direct response",
			req:  ExecuteRequest{Input: "hello, what can you do?"},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "plain question uses direct response",
			req:  ExecuteRequest{Input: "解释一下 Acorn 是什么"},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "blank input uses direct response",
			req:  ExecuteRequest{Input: " \n\t "},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "repo task without explicit mode uses direct response",
			req:  ExecuteRequest{Input: "修复 internal/runtime/executor_run.go 并跑 go test"},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "verification intent without explicit mode uses direct response",
			req:  ExecuteRequest{Input: "验证这个改动并运行 make lint"},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "english mutation intent without explicit mode uses direct response",
			req:  ExecuteRequest{Input: "implement the new trace drawer behavior"},
			want: orchestrationmode.DirectResponse,
		},
		{
			name: "explicit mode is preserved",
			req: ExecuteRequest{
				Input:             "你好",
				OrchestrationMode: orchestrationmode.PlanExecute,
			},
			want: orchestrationmode.PlanExecute,
		},
		{
			name: "unknown explicit mode is preserved for unsupported-mode failure",
			req: ExecuteRequest{
				Input:             "你好",
				OrchestrationMode: orchestrationmode.Mode("unknown_mode"),
			},
			want: orchestrationmode.Mode("unknown_mode"),
		},
		{
			name: "child run defaults to single agent",
			req: ExecuteRequest{
				Input:       "inspect",
				ParentRunID: "run_parent",
			},
			want: orchestrationmode.SingleAgent,
		},
		{
			name: "explicit skill uses plan execute",
			req: ExecuteRequest{
				Input:   "处理这个",
				SkillID: "cs-feat-impl",
			},
			want: orchestrationmode.PlanExecute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveRootOrchestrationMode(tt.req); got != tt.want {
				t.Fatalf("resolveRootOrchestrationMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
