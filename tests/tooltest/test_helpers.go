package tooltest

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

// MustInferTool infers a tool from a function and fails the test on error.
func MustInferTool[T any, R any](t testing.TB, name string, fn func(context.Context, T) (R, error)) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, fn)
	if err != nil {
		t.Fatalf("infer tool: %v", err)
	}
	return tool
}
