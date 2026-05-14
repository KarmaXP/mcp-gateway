package multiplex

import "context"

func (a *Multiplexer) acquireGlobalCallSlot(ctx context.Context) (func(), error) {
	if a == nil || a.globalCallSemaphore == nil {
		return func() {}, nil
	}
	if err := a.globalCallSemaphore.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() {
		a.globalCallSemaphore.Release(1)
	}, nil
}
