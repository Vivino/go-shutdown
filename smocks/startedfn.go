package smocks

import "context"

// StartedFn method always returns true, and channels are never closed
type StartedFn struct{}

// Started returns true
func (StartedFn) Started() bool { return true }

// StartedCh returns a níl channel
func (StartedFn) StartedCh() <-chan struct{} { return nil }

// CancelCtx mimics canceling context on shutdown, but never does so
func (StartedFn) CancelCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
