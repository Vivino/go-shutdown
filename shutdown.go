// Copyright (c) 2015 Klaus Post, 2023 Eik Madsen, released under MIT License. See LICENSE file.

// Package shutdown provides management of your shutdown process.
//
// The package will enable you to get notifications for your application and handle the shutdown process.
//
// # See more information about the how to use it in the README.md file
//
// Package home: https://github.com/Vivino/go-shutdown
package shutdown

// Stage contains stage information.
// Valid values for this are exported as variables StageN.
type Stage struct {
	n int
}

// LogPrinter is an interface for writing logging information.
// The writer must handle concurrent writes.
type LogPrinter interface {
	Printf(format string, v ...interface{})
}

// Internal notifier
type iNotifier struct {
	n          Notifier
	calledFrom string
}
type fnNotify struct {
	client   Notifier
	internal iNotifier
	cancel   chan struct{}
}

type logWrapper struct {
	w func(format string, v ...interface{})
}

func (l logWrapper) Printf(format string, v ...interface{}) {
	l.w(format, v...)
}

// Notifier is a channel, that will be sent a channel
// once the application shuts down.
// When you have performed your shutdown actions close the channel you are given.
type Notifier struct {
	c chan chan struct{}
	m *Manager
}

// Valid returns true if it can be used as a notifier. If false shutdown has already started
func (n Notifier) Valid() bool {
	return n.c != nil && n.m != nil
}

// Notify returns a channel to listen to for shutdown events.
func (n Notifier) Notify() <-chan chan struct{} {
	return n.c
}

// Cancel a Notifier.
// This will remove a notifier from the shutdown queue,
// and it will not be signalled when shutdown starts.
// If the shutdown has already started this will not have any effect,
// but a goroutine will wait for the notifier to be triggered.
func (s Notifier) Cancel() {
	if !s.Valid() {
		return
	}
	s.m.srM.RLock()
	if s.m.shutdownRequested.Load() {
		s.m.srM.RUnlock()
		// Wait until we get the notification and close it:
		go func() {
			v := <-s.c
			close(v)
		}()
		return
	}
	s.m.srM.RUnlock()
	s.m.sqM.Lock()
	var a chan chan struct{}
	var b chan chan struct{}
	a = s.c
	for n, sdq := range s.m.shutdownQueue {
		for i, qi := range sdq {
			b = qi.n.c
			if a == b {
				s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
			}
		}
		for i, fn := range s.m.shutdownFnQueue[n] {
			b = fn.client.c
			if a == b {
				// Find the matching internal and remove that.
				for i := range s.m.shutdownQueue[n] {
					b = s.m.shutdownQueue[n][i].n.c
					if fn.internal.n.c == b {
						s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
						break
					}
				}
				// Cancel, so the goroutine exits.
				close(fn.cancel)
				// Remove this
				s.m.shutdownFnQueue[n] = append(s.m.shutdownFnQueue[n][:i], s.m.shutdownFnQueue[n][i+1:]...)
			}
		}
	}
	s.m.sqM.Unlock()
}

// CancelWait will cancel a Notifier, or wait for it to become active if shutdown has been started.
// This will remove a notifier from the shutdown queue, and it will not be signalled when shutdown starts.
// If the notifier is invalid (requested after its stage has started), it will return at once.
// If the shutdown has already started, this will wait for the notifier to be called and close it.
func (s Notifier) CancelWait() {
	if !s.Valid() {
		return
	}
	s.m.sqM.Lock()
	var a chan chan struct{}
	var b chan chan struct{}
	a = s.c
	for n, sdq := range s.m.shutdownQueue {
		for i, qi := range sdq {
			b = qi.n.c
			if a == b {
				s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
			}
		}
		for i, fn := range s.m.shutdownFnQueue[n] {
			b = fn.client.c
			if a == b {
				// Find the matching internal and remove that.
				for i := range s.m.shutdownQueue[n] {
					b = s.m.shutdownQueue[n][i].n.c
					if fn.internal.n.c == b {
						s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
						break
					}
				}
				// Cancel, so the goroutine exits.
				close(fn.cancel)
				// Remove this
				s.m.shutdownFnQueue[n] = append(s.m.shutdownFnQueue[n][:i], s.m.shutdownFnQueue[n][i+1:]...)
			}
		}
	}
	s.m.srM.Lock()
	if s.m.shutdownRequested.Load() {
		s.m.sqM.Unlock()
		s.m.srM.Unlock()
		// Wait until we get the notification and close it:
		v := <-s.c
		close(v)

		return
	}
	s.m.srM.Unlock()
	s.m.sqM.Unlock()
}
