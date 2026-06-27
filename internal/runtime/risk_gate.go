package runtime

import (
	"errors"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// ApprovalRequiredError signals that a tool call was intercepted by the risk
// gate because it is high-risk and must be explicitly approved by the operator
// via ask_operator. The run does NOT fail — the agent receives a tool result
// telling it to request approval, and the loop continues.
type ApprovalRequiredError struct {
	ToolName string
	CallID   string
}

func (e *ApprovalRequiredError) Error() string {
	return fmt.Sprintf("tool %q requires operator approval (risk gate)", e.ToolName)
}

// IsApprovalRequiredError reports whether err is an ApprovalRequiredError.
func IsApprovalRequiredError(err error) bool {
	var are *ApprovalRequiredError
	return errors.As(err, &are)
}

// approvalRequiredToolMessage builds the tool result message that tells the
// model why the call was blocked and what to do next. The model sees this as a
// normal tool result and can decide to call ask_operator.
func approvalRequiredToolMessage(callID, toolName string) *schema.Message {
	msg := schema.ToolMessage(fmt.Sprintf(
		"This operation (%s) is high-risk and requires operator approval before execution. "+
			"Use the ask_operator tool to request explicit approval, then retry if approved.",
		toolName,
	), callID)
	return msg
}
