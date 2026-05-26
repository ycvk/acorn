package runtime

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
)

func (f *RunnerFactory) installRunChatModelBuilderForTest(fn func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)) {
	if f == nil {
		return
	}
	f.runChatModelBuilder = fn
}
