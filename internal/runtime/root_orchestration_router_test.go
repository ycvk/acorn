package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/orchestrationmode"
)

func TestResolveRootOrchestrationMode(t *testing.T) {
	tests := []struct {
		name string
		req  ExecuteRequest
		want orchestration.OrchestrationMode
	}{
		{
			name: "greeting uses direct response",
			req:  ExecuteRequest{Input: "你好"},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "english greeting uses direct response",
			req:  ExecuteRequest{Input: "hello, what can you do?"},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "plain question uses direct response",
			req:  ExecuteRequest{Input: "解释一下 Acorn 是什么"},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "blank input uses direct response",
			req:  ExecuteRequest{Input: " \n\t "},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "repo task without explicit mode uses direct response",
			req:  ExecuteRequest{Input: "修复 internal/runtime/executor_run.go 并跑 go test"},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "verification intent without explicit mode uses direct response",
			req:  ExecuteRequest{Input: "验证这个改动并运行 make lint"},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "english mutation intent without explicit mode uses direct response",
			req:  ExecuteRequest{Input: "implement the new trace drawer behavior"},
			want: orchestration.OrchestrationModeDirectResponse,
		},
		{
			name: "explicit mode is preserved",
			req: ExecuteRequest{
				Input:             "你好",
				OrchestrationMode: orchestrationmode.PlanExecute,
			},
			want: orchestration.OrchestrationModePlanExecute,
		},
		{
			name: "unknown explicit mode is preserved for unsupported-mode failure",
			req: ExecuteRequest{
				Input:             "你好",
				OrchestrationMode: orchestrationmode.Mode("unknown_mode"),
			},
			want: orchestration.OrchestrationMode("unknown_mode"),
		},
		{
			name: "child run defaults to single agent",
			req: ExecuteRequest{
				Input:       "inspect",
				ParentRunID: "run_parent",
			},
			want: orchestration.OrchestrationModeSingleAgent,
		},
		{
			name: "explicit skill uses plan execute",
			req: ExecuteRequest{
				Input:   "处理这个",
				SkillID: "cs-feat-impl",
			},
			want: orchestration.OrchestrationModePlanExecute,
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
