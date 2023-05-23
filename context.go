// Copyright (c) 2015 Klaus Post, released under MIT License. See LICENSE file.

//go:build go1.7
// +build go1.7

package shutdown

import "context"

// CancelCtx will cancel the supplied context when shutdown starts.
// The returned context must be cancelled when done similar to
// https://golang.org/pkg/context/#WithCancel
func (m *Manager) CancelCtx(parent context.Context) (ctx context.Context, cancel context.CancelFunc) {
	return m.cancelContext(parent, m.StagePS)
}

// CancelCtxN will cancel the supplied context at a supplied shutdown stage.
// The returned context must be cancelled when done similar to
// https://golang.org/pkg/context/#WithCancel
func (m *Manager) CancelCtxN(parent context.Context, s Stage) (ctx context.Context, cancel context.CancelFunc) {
	return m.cancelContext(parent, s)
}

func (m *Manager) cancelContext(parent context.Context, s Stage) (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithCancel(parent)
	f := m.onShutdown(s.n, 2, []interface{}{parent}).n
	if !f.Valid() {
		cancel()
		return ctx, cancel
	}
	go func() {
		select {
		case <-ctx.Done():
			f.CancelWait()
		case v := <-f.Notify():
			cancel()
			close(v)
		}
	}()
	return ctx, cancel
}
