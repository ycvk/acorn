package runtime

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
)

func (f *RunnerFactory) installRunChatModelBuilderForTest(fn func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)) {
	if f == nil || f.runBuilder == nil || f.runBuilder.modelProvider == nil {
		return
	}
	f.runBuilder.modelProvider.SetBuildRunForTest(fn)
}
