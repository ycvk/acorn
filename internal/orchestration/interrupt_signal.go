package orchestration

import "github.com/cloudwego/eino/adk"

func InterruptInfoFromSignal(signal *adk.InterruptSignal) *adk.InterruptInfo {
	if signal == nil {
		return nil
	}
	info := &adk.InterruptInfo{
		Data: signal.InterruptInfo.Info,
	}
	if len(signal.Subs) > 0 {
		info.InterruptContexts = interruptCtxsFromSignals(signal.Subs)
		return info
	}
	info.InterruptContexts = []*adk.InterruptCtx{
		{
			ID:          signal.ID,
			Address:     signal.Address,
			Info:        signal.InterruptInfo.Info,
			IsRootCause: signal.InterruptInfo.IsRootCause,
		},
	}
	return info
}

func interruptCtxsFromSignals(signals []*adk.InterruptSignal) []*adk.InterruptCtx {
	ctxs := make([]*adk.InterruptCtx, 0, len(signals))
	for _, s := range signals {
		if s == nil {
			continue
		}
		ctxs = append(ctxs, &adk.InterruptCtx{
			ID:          s.ID,
			Address:     s.Address,
			Info:        s.InterruptInfo.Info,
			IsRootCause: s.InterruptInfo.IsRootCause,
		})
		if len(s.Subs) > 0 {
			ctxs = append(ctxs, interruptCtxsFromSignals(s.Subs)...)
		}
	}
	return ctxs
}
