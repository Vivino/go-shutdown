package smocks

import "context"

// NeverStarts method always returns false, and channels are never closed
type NeverStarts struct{}

// Started returns false
func (NeverStarts) Started() bool { return false }

// StartedCh returns a níl channel
func (NeverStarts) StartedCh() <-chan struct{} { return nil }

// CancelCtx mimics canceling context on shutdown, but never does so
func (NeverStarts) CancelCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
